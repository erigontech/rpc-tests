#!/usr/bin/python3
"""
This script connects to an Ethereum node via HTTP and sends requests via GraphQL.
"""

import aiohttp
import argparse
import asyncio
import json
import logging
import pathlib
import shutil
import signal
import sys
import tempfile
import urllib.parse

from .common import graphql

# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)


def parse_github_tree_url(url: str) -> tuple[str, str, str, str]:
    """Helper to parse the GitHub URL: extracts owner, repo, branch, and path from a GitHub tree URL."""
    try:
        parts = urllib.parse.urlparse(url).path.strip('/').split('/')
        if len(parts) < 5 or parts[2] != 'tree':
            raise ValueError(f"Invalid GitHub 'tree' URL format: {url}.")

        owner, repo, _, branch, *path_parts = parts
        folder_path = '/'.join(path_parts)
        return owner, repo, branch, folder_path
    except Exception as e:
        raise ValueError(f"Failed to parse GitHub URL: {url}. Error: {e}")


async def download_github_directory(url: str) -> pathlib.Path:
    """Downloads all files from specified GitHub URL into a temporary local folder using GitHub REST API."""
    owner, repo, branch, folder_path = parse_github_tree_url(url)
    api_url = f"https://api.github.com/repos/{owner}/{repo}/contents/{folder_path}?ref={branch}"

    try:
        # Create a secure, temporary directory
        temp_dir = pathlib.Path(tempfile.mkdtemp())
        logger.info(f"⬇️ Downloading test files to temporary directory: {temp_dir}")

        # Use aiohttp for async API calls
        async with aiohttp.ClientSession() as session:
            # Request directory contents
            async with session.get(api_url) as response:
                if response.status != aiohttp.http.HTTPStatus.OK:
                    content = await response.text()
                    raise Exception(f"GitHub API error {response.status}. URL: {api_url}. Response: {content[:100]}...")

                # The API returns a list of files/directories
                contents = await response.json()

            download_tasks = []
            for item in contents:
                # Test files are expected to be plain JSON files
                if item['type'] == 'file' and item['name'].endswith('.json'):
                    # Get the raw download URL
                    download_url = item['download_url']
                    file_path = temp_dir / item['name']

                    # Create a task to download each file concurrently
                    task = download_single_file(session, download_url, file_path)
                    download_tasks.append(task)

            # Wait for all file downloads to complete
            await asyncio.gather(*download_tasks)
            logger.info(f"✅ Downloaded {len(download_tasks)} test files.")
            return temp_dir
    except Exception as e:
        logger.error(f"❌ Error during GitHub download: {e}")
        raise e


async def download_single_file(session: aiohttp.ClientSession, url: str, path: pathlib.Path):
    """Downloads a single file asynchronously and saves it to the specified path."""
    async with session.get(url) as response:
        response.raise_for_status()  # Raises an exception for bad status codes (4xx or 5xx)
        content = await response.read()
        path.write_bytes(content)
        logger.debug(f"Downloaded {path.name}")


async def execute_tests(processor: graphql.QueryProcessor, url: str, stop_at_error: bool, test_number: int | None):
    """Downloads files from the specified URL, discovers test files, and executes them."""
    temp_dir = None
    try:
        # Download the GitHub directory contents into a temporary folder
        temp_dir = await download_github_directory(url)

        logger.info(f"Starting test execution using files from {temp_dir}")

        # Discover test files (only looking for .json files) and sort them by name
        test_files = list(temp_dir.glob('*.json'))
        if not test_files:
            logger.warning(f"⚠️ No *.json files found in {temp_dir}. Aborting tests.")
            return 1
        test_files = sorted(test_files)

        total_tests = len(test_files) if not test_number else 1
        passed_tests = 0

        # Iterate and execute all tests by establishing and reusing the same session
        async with processor as session:
            for i, test_file_path in enumerate(test_files):
                if test_number and test_number != i:
                    continue
                test_name = test_file_path.name
                try:
                    test_case = json.loads(test_file_path.read_text(encoding='utf-8'))

                    query = test_case.get("request", "").strip()
                    if not query:
                        logger.error(f"❌ Test {i + 1} FAILED: 'request' field is missing in {test_name}.")
                        continue
                    expected_responses = test_case.get("responses", [])
                    if not expected_responses:
                        logger.error(f"❌ Test {i + 1} FAILED: 'responses' field is missing in {test_name}.")
                        continue
                except json.JSONDecodeError as e:
                    logger.error(f"❌ Test {i + 1} FAILED: Invalid JSON format in {test_name}: {e}")
                    continue

                # Execute the query
                actual_result = await session.execute(query)

                # Compare actual vs expected: the test passes if the actual result matches ANY of the expected responses
                is_passing = False
                actual_data, actual_errors = actual_result.data, actual_result.errors
                expected_data, expected_errors = None, None
                for expected in expected_responses:
                    # Assume the expected response contains { "data": { ... } } or { "errors": { ... } } or both
                    expected_data = expected.get("data")
                    if actual_data == expected_data:
                        is_passing = True
                        break  # Found a match, test passes
                    expected_errors = expected.get("errors")
                    if expected_errors and actual_errors:
                        is_passing = True
                        break  # Found a match, test passes

                # Log result and update counters
                if is_passing:
                    passed_tests += 1
                    logger.info(f"✅ Test {i + 1} {test_name} PASSED.")
                else:
                    logger.info(f"❌ Test {i + 1} {test_name} FAILED: actual result didn't match any expected response.")
                    logger.warning(f"Request: {query}")
                    if expected_data:
                        logger.warning(f"Expected data: {expected_data}")
                        if actual_data:
                            logger.warning(f"Actual data:   {actual_data}")
                        else:
                            logger.warning(f"Actual error:  {actual_errors}")
                    else:
                        logger.warning(f"Expected error: {expected_errors}")
                        if actual_errors:
                            logger.warning(f"Actual error:  {actual_errors}")
                        else:
                            logger.warning(f"Actual data:   {actual_data}")
                    if stop_at_error:
                        logger.info(f"Testing finished after first error. Passed: {passed_tests}/{total_tests}")
                        return 1

            logger.info(f"Testing finished. Passed: {passed_tests}/{total_tests}")
            status = 0 if passed_tests == total_tests else 1
            return status

    except (ValueError, Exception) as e:
        logger.error(f"❌ Test setup/execution failed: {e}")
        return 1

    finally:
        # Clean up the temporary directory created by the download function
        if temp_dir and temp_dir.exists():
            logger.info(f"Cleaning up temporary directory: {temp_dir}")
            try:
                shutil.rmtree(temp_dir)
            except OSError as e:
                logger.error(f"Failed to remove temporary directory {temp_dir}: {e}")


async def main():
    """Main function to run the GraphQL processor."""

    # Parse command-line arguments
    parser = argparse.ArgumentParser(
        description="Queries an Ethereum node via GraphQL."
    )
    parser.add_argument(
        "--http_url",
        type=str,
        default="http://127.0.0.1:8545/graphql",
        help="The GraphQL URL of the Ethereum node (default: http://127.0.0.1:8545/graphql)",
    )
    parser.add_argument(
        "--stop_at_first_error",
        action="store_true",
        default=False,
        help="Flag indicating that execution must be stopped at first error",
    )
    parser.add_argument(
        "--test_number",
        type=int,
        default=None,
        help="The progressive number of the test file to execute. If specified, only such test gets executed",
    )
    # User MUST provide EITHER --query OR --tests_url, but NOT BOTH.
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument(
        "--query",
        type=str,
        default=None,
        help="The GraphQL query as string",
    )
    group.add_argument(
        "--tests_url",
        type=str,
        default=None,
        help="The URL or path indicating where to read test files",
    )
    args = parser.parse_args()

    # Create the GraphQL query processor
    processor = graphql.QueryProcessor(args.http_url)

    # Setup signal handler for graceful shutdown
    async def signal_handler():
        print("")
        logger.info("🏁 Received interrupt signal (Ctrl+C)")
        await processor.close()

    loop = asyncio.get_running_loop()
    for sig in [signal.SIGINT, signal.SIGTERM]:
        loop.add_signal_handler(sig, lambda: asyncio.create_task(signal_handler()))

    status = 0
    try:
        if args.query:
            result = await processor.execute_async(args.query)
            logger.info(f"✅ Result: {result}")
        else:
            status = await execute_tests(processor, args.tests_url, args.stop_at_first_error, args.test_number)
    except KeyboardInterrupt:
        logger.info("Interrupted by user")
    except Exception as e:
        logger.error(f"❌ Unexpected error: {e}")
        status = e
    finally:
        sys.exit(status)


if __name__ == "__main__":
    """ Usage: python graphql.py """
    asyncio.run(main())
