#!/usr/bin/python3
""" Copy the response json from result dir into rpc-tests dir """


import os
import re
import json
import tarfile
import glob
import sys

# --- Funzioni di Supporto ---

def extract_number(filename):
    """ extract number from test namee """
    match = re.search(r'\d+', filename)
    if match:
        return int(match.group())
    return 0

def find_and_read_response_json(search_dir, test_base_name):
    """ 
    It searches for and reads the JSON response file, returning the entire object or list of responses.
    """
    if not os.path.isdir(search_dir):
        print(f"Error: Dir '{search_dir}' NOT exist.")
        return None

    search_pattern = os.path.join(search_dir, f"{test_base_name}*response*.json")
    found_files = glob.glob(search_pattern)

    if not found_files:
        print(f"No file found according pattern '{test_base_name}*response*.json' in {search_dir}")
        return None

    file_path = found_files[0]
    print(f"File is found {file_path}")

    try:
        with open(file_path, 'r', encoding='utf-8') as f:
            data = json.load(f)

        # Function to validate a single JSON-RPC response object
        def is_valid_jsonrpc_response(item):
            if not isinstance(item, dict):
                return False
            # Check for 'id' and 'jsonrpc' keys
            if 'id' not in item or 'jsonrpc' not in item:
                return False
            # Check for either 'result' or 'error' key
            if ('result' not in item and 'error' not in item) or \
               ('result' in item and 'error' in item):
                return False
            return True

        # Check for both a single object and a list of objects
        if isinstance(data, list):
            if data and all(is_valid_jsonrpc_response(item) for item in data):
                print(f"Find valid JSON-RPC response in: {file_path}")
                return data
        elif is_valid_jsonrpc_response(data):
            print(f"Find valid JSON-RPC response in: {file_path}")
            return data
        print(f"File {file_path}' is NOT valid.")
        return None
    except (json.JSONDecodeError, FileNotFoundError) as e:
        print(f"Error: during read of the file {file_path}: {e}")
        return None

def update_response_in_json_data(data, new_response_data, test_base_name):
    """
    Update the response in the JSON, handling both dictionaries and lists. 
    Do not update if the original response has a null 'result' field or is missing the 'result' field. 
    Return True if the content was modified, False otherwise
    """
    if not new_response_data:
        print(f"Empty response data for '{test_base_name}'. No update.")
        return False

    modified = False

    def should_not_update(current_response):
        """
        The response should be updated if:
           It contains a non-null 'result' field (success response).
           It contains a non-null 'error' field (failure/RPC error response).

        DO NOT update (return True) if:
           The format is not a valid dictionary.
           It is missing both the 'result' and 'error' fields (incomplete or empty RPC response).
           It has the 'result' field but it is null, and the 'error' field is missing.
        """
        if not isinstance(current_response, dict):
            return False
        has_valid_result = "result" in current_response and current_response.get("result") is not None
        has_valid_error = "error" in current_response and current_response.get("error") is not None
        if has_valid_result or has_valid_error:
            return False
        return True

    if isinstance(data, dict) and 'response' in data:
        current_response = data['response']
        if not should_not_update(current_response):
            if current_response != new_response_data:
                data['response'] = new_response_data
                modified = True
                print(f"Response updated in '{test_base_name}'.")
            else:
                print(f" File not updated '{test_base_name}'. Contains correct data.")
        else:
            print(f"The original response is empty or has result=null for '{test_base_name}'. It will not be updated.")

    elif isinstance(data, list):
        for item in data:
            if isinstance(item, dict) and 'response' in item:
                current_response = item['response']
                if not should_not_update(current_response):
                    if current_response != new_response_data:
                        item['response'] = new_response_data
                        modified = True
                        print(f"Response updated in '{test_base_name}' (list of response).")
                        break
                    print(f" File not updated '{test_base_name}'. Contains correct data.")
                    break
                print(f"The original response is empty or has result=null for '{test_base_name}'. It will not be updated.")
                break

        if not modified and len(data) > 0 and 'result' not in data[0] and 'error' not in data[0]:
            pass

    return modified

# --- process_single_test_json_for_response_sync (rimane invariata, usa update_response_in_json_data) ---
def process_single_test_json_for_response_sync(filepath, result_api_dir):
    """ process single test json file """
    print(f"Processing of file: {filepath}")

    try:
        with open(filepath, 'r', encoding='utf-8') as f:
            data = json.load(f)

        test_base_name = os.path.splitext(os.path.basename(filepath))[0]
        full_response_from_result = find_and_read_response_json(result_api_dir, test_base_name)
        was_modified = update_response_in_json_data(data, full_response_from_result, test_base_name)
        if was_modified:
            with open(filepath, 'w', encoding='utf-8') as f_out:
                json.dump(data, f_out, indent=2)
                print(f"  **{os.path.basename(filepath)}** updated.")
        else:
            print(f"  No change for {os.path.basename(filepath)}. File is not written.")
        return was_modified

    except json.JSONDecodeError as e:
        print(f"Error: in decode of file {filepath}: {e}.")
        return False
    except Exception as e:
        print(f"Error: in procesing of file {filepath}: {e}.")
        return False

def process_tar_file_for_response_sync(filepath, result_api_dir):
    """ process tar file """
    print(f"Processing of TAR file: {filepath}")
    tar_dirname = os.path.dirname(filepath)
    tar_basename = os.path.basename(filepath)
    tar_name_without_ext = os.path.splitext(tar_basename)[0]
    temp_json_file_path_for_tar_add = os.path.join(tar_dirname, f"{tar_name_without_ext}_temp.json")
    try:
        with tarfile.open(filepath, 'r') as tar:
            internal_json_filename = None
            for member in tar.getmembers():
                if member.name.endswith('.json') and member.name.startswith(tar_name_without_ext):
                    internal_json_filename = member.name
                    break

            if not internal_json_filename:
                print(f"  Error: No file found '{tar_name_without_ext}' in TAR.")
                return False

            print(f"  Extracting '{internal_json_filename}' from TAR...")
            extracted_json_file_obj = tar.extractfile(internal_json_filename)
            if extracted_json_file_obj:
                json_content_str_original = extracted_json_file_obj.read().decode('utf-8')
                extracted_json_file_obj.close()
            else:
                print(f"Failed to read the content of '{internal_json_filename}' from '{tar_basename}'. Skipping.")
                return False

        data = json.loads(json_content_str_original)

        test_base_name = tar_name_without_ext

        full_response_from_result = find_and_read_response_json(result_api_dir, test_base_name)

        tar_content_modified = update_response_in_json_data(data, full_response_from_result, test_base_name)
        if tar_content_modified:
            print(f"  TAR re-created '{tar_basename}' with updated json...")
            with open(temp_json_file_path_for_tar_add, 'w', encoding='utf-8') as temp_f:
                json.dump(data, temp_f, indent=4)
            with tarfile.open(filepath, 'w:bz2') as new_tar:
                new_tar.add(temp_json_file_path_for_tar_add, arcname=internal_json_filename)
            print(f"  File TAR updated: {filepath}")
        else:
            print(f"  JSON file not modified {filepath}.")

    except tarfile.ReadError:
        print(f"Error: Failed to read the TAR file {filepath}. It may be corrupt or not a valid tar archive.")
    except Exception as e:
        print(f"  Error:  during processing TAR {filepath}: {e}")
    finally:
        if os.path.exists(temp_json_file_path_for_tar_add):
            try:
                os.remove(temp_json_file_path_for_tar_add)
            except Exception as e:
                print(f" Error cleaning up temporary file {os.path.basename(temp_json_file_path_for_tar_add)}: {e}")
    return False

def main():
    """ main """
    # 1. Gestione dell'argomento da riga di comando
    if len(sys.argv) < 2:
        print("Usage: python your_script_name.py <chain>")
        print("Supported chains: mainnet, gnosis")
        sys.exit(1)

    chain = sys.argv[1]
    if chain not in ['mainnet', 'gnosis']:
        print(f"Error: Unsupported chain '{chain}'. Supported chains are 'mainnet' and 'gnosis'.")
        sys.exit(1)

    # 2. Impostazione dinamica dei percorsi in base alla chain scelta
    base_dir = os.getcwd()
    chain_dir = os.path.join(base_dir, chain)
    result_dir = os.path.join(chain_dir, 'results')

    # Controlla se il percorso termina con 'integration'
    if not base_dir.endswith('integration'):
        print("DIR corrente terminated with 'integration':", base_dir)
        return
    if not os.path.isdir(chain_dir):
        print(f"Error: The directory for chain '{chain}' specified '{chain_dir}' does not exist.")
        return
    if not os.path.isdir(result_dir):
        print(f"Error: The 'results' directory '{result_dir}' does not exist.")
        return

    api_subdirs_in_result = [d for d in os.listdir(result_dir) if os.path.isdir(os.path.join(result_dir, d))]

    if len(api_subdirs_in_result) != 1:
        print(f"Error: Expected exactly ONE API subdirectory in '{result_dir}', but found {len(api_subdirs_in_result)}.")
        print(f"Ensure '{result_dir}' contains only one test results directory at a time.")
        return

    api_name = api_subdirs_in_result[0]
    result_api_source_dir = os.path.join(result_dir, api_name)
    chain_target_api_dir = os.path.join(chain_dir, api_name)

    if not os.path.isdir(chain_target_api_dir):
        print(f"Warning: The corresponding API directory in '{chain}' ('{chain_target_api_dir}') does not exist. Skipping processing for this API.")
        return

    print(f"*** Starting 'response' section synchronization for API '{api_name}' on '{chain}' ***")
    print(f"    (Data taken from '{result_api_source_dir}' and applied to '{chain_target_api_dir}')")
    print("=" * 70)

    total_files_processed = 0

    for root, _, files in os.walk(chain_target_api_dir):
        sorted_files_in_dir = sorted(files, key=extract_number)
        for filename in sorted_files_in_dir:
            filepath = os.path.join(root, filename)

            if filename.endswith('.json'):
                print("-" * 50)
                process_single_test_json_for_response_sync(filepath, result_api_source_dir)
                total_files_processed += 1
            elif filename.endswith('.tar'):
                print("-" * 50)
                process_tar_file_for_response_sync(filepath, result_api_source_dir)
                total_files_processed += 1

    if total_files_processed == 0:
        print(f"No .json or .tar files found in '{chain_target_api_dir}' or its subdirectories.")
    else:
        print(f"\nSynchronization completed. Total files processed: {total_files_processed}.")
        print("=" * 70)

if __name__ == "__main__":
    main()
