#!/usr/bin/env python3
"""
Ethereum Transaction Sender using eth_sendRawTransactionSync

This script demonstrates:
- Async/await throughout the entire codebase
- Creating a new Ethereum wallet (optional)
- Requesting testnet ETH from a Sepolia faucet (optional)
- Connecting to an Ethereum node that supports EIP-7966
- Sending a raw transaction using eth_sendRawTransactionSync (EIP-7966) with async HTTP
- The method waits synchronously and returns the receipt directly

Note: eth_sendRawTransactionSync is from EIP-7966 and may not be available on all nodes.
Compatible with: QuickNode (with add-on), some L2s, and custom implementations.
"""

import aiohttp
import argparse
import asyncio
import json
import logging
import sys
import time

from aiohttp.client_exceptions import ClientConnectorError
from eth_account import Account
from eth_account.signers.local import LocalAccount
from typing import Optional, Dict, Any
from web3 import Web3

# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)


class WalletManager:
    """Manages Ethereum wallet creation and storage"""

    @staticmethod
    def create_wallet() -> LocalAccount:
        """Create a new Ethereum wallet"""
        Account.enable_unaudited_hdwallet_features()
        account: LocalAccount = Account.create()
        logger.info(f"✅ New wallet created")
        logger.info(f"   Address: {account.address}")
        logger.info(f"   Private Key: {account.key.hex()}")
        logger.warning("⚠️  SAVE YOUR PRIVATE KEY SECURELY - Never share it!")
        return account

    @staticmethod
    def load_wallet(private_key: str) -> LocalAccount:
        """Load wallet from private key"""
        try:
            account: LocalAccount = Account.from_key(private_key)
            logger.info(f"✅ Wallet loaded: {account.address}")
            return account
        except Exception as e:
            logger.error(f"❌ Failed to load wallet: {e}")
            raise


class SepoliaFaucet:
    """Interact with Sepolia testnet faucets"""

    # Popular Sepolia faucets
    FAUCETS = {
        "alchemy": "https://sepoliafaucet.com",
        "infura": "https://www.infura.io/faucet/sepolia",
        "chainlink": "https://faucets.chain.link/sepolia",
        "quicknode": "https://faucet.quicknode.com/ethereum/sepolia"
    }

    @staticmethod
    def request_funds_manual(address: str):
        """
        Print instructions for manually requesting funds from faucets.
        Most faucets require manual interaction (captcha, etc.)
        """
        logger.info("=" * 70)
        logger.info("📝 MANUAL FAUCET REQUEST REQUIRED")
        logger.info("=" * 70)
        logger.info(f"Your wallet address: {address}")
        logger.info("\nPlease visit one of these Sepolia faucets:")
        for name, url in SepoliaFaucet.FAUCETS.items():
            logger.info(f"  • {name.capitalize()}: {url}")
        logger.info("\nAfter requesting funds, wait a few minutes then run the script again.")
        logger.info("=" * 70)

    @staticmethod
    async def wait_for_balance(
        session: aiohttp.ClientSession,
        rpc_url: str,
        address: str,
        timeout: int = 300
    ) -> bool:
        """Wait until the address has a non-zero balance"""
        logger.info(f"⏳ Waiting for balance (timeout: {timeout}s)...")
        start_time = time.time()

        while time.time() - start_time < timeout:
            try:
                balance = await AsyncWeb3Helper.get_balance(session, rpc_url, address)
                if balance > 0:
                    balance_eth = Web3.from_wei(balance, 'ether')
                    logger.info(f"✅ Balance received: {balance_eth} ETH")
                    return True
            except Exception as e:
                logger.debug(f"Error checking balance: {e}")

            await asyncio.sleep(5)
            elapsed = int(time.time() - start_time)
            logger.info(f"   Still waiting... ({elapsed}s elapsed)")

        logger.error(f"❌ Timeout: No balance received after {timeout}s")
        return False


class AsyncWeb3Helper:
    """Async helper functions for Web3 operations using aiohttp"""

    @staticmethod
    async def json_rpc_call(
        session: aiohttp.ClientSession,
        rpc_url: str,
        method: str,
        params: list,
        timeout: Optional[int] = None
    ) -> Dict[str, Any]:
        """Make an async JSON-RPC call"""
        payload = {
            "jsonrpc": "2.0",
            "method": method,
            "params": params,
            "id": 1
        }

        timeout_obj = aiohttp.ClientTimeout(total=timeout) if timeout else aiohttp.ClientTimeout(total=120)

        async with session.post(
            rpc_url,
            json=payload,
            headers={'Content-Type': 'application/json'},
            timeout=timeout_obj
        ) as response:
            response.raise_for_status()
            return await response.json()

    @staticmethod
    async def get_balance(
        session: aiohttp.ClientSession,
        rpc_url: str,
        address: str
    ) -> int:
        """Get balance of an address"""
        result = await AsyncWeb3Helper.json_rpc_call(
            session, rpc_url, "eth_getBalance", [address, "latest"]
        )
        if 'error' in result:
            raise Exception(f"RPC error: {result['error']}")
        balance_hex = result['result']
        return int(balance_hex, 16)

    @staticmethod
    async def get_transaction_count(
        session: aiohttp.ClientSession,
        rpc_url: str,
        address: str
    ) -> int:
        """Get transaction count (nonce) for an address"""
        result = await AsyncWeb3Helper.json_rpc_call(
            session, rpc_url, "eth_getTransactionCount", [address, "latest"]
        )
        if 'error' in result:
            raise Exception(f"RPC error: {result['error']}")
        nonce_hex = result['result']
        return int(nonce_hex, 16)

    @staticmethod
    async def get_chain_id(
        session: aiohttp.ClientSession,
        rpc_url: str
    ) -> int:
        """Get chain ID"""
        result = await AsyncWeb3Helper.json_rpc_call(
            session, rpc_url, "eth_chainId", []
        )
        if 'error' in result:
            raise Exception(f"RPC error: {result['error']}")
        chain_id_hex = result['result']
        return int(chain_id_hex, 16)

    @staticmethod
    async def get_block(
        session: aiohttp.ClientSession,
        rpc_url: str,
        block: str = "latest"
    ) -> Dict[str, Any]:
        """Get block information"""
        result = await AsyncWeb3Helper.json_rpc_call(
            session, rpc_url, "eth_getBlockByNumber", [block, False]
        )
        if 'error' in result:
            raise Exception(f"RPC error: {result['error']}")
        return result['result']

    @staticmethod
    async def get_block_number(
            session: aiohttp.ClientSession,
            rpc_url: str
    ) -> int:
        """Get latest block number"""
        result = await AsyncWeb3Helper.json_rpc_call(
            session, rpc_url, "eth_blockNumber", []
        )
        if 'error' in result:
            raise Exception(f"RPC error: {result['error']}")
        block_hex = result['result']
        return int(block_hex, 16)


class AsyncTransactionSender:
    """Handles Ethereum transaction creation and submission using eth_sendRawTransactionSync"""

    def __init__(self, account: LocalAccount, rpc_url: str):
        self.account = account
        self.rpc_url = rpc_url
        self.w3 = Web3()

    async def create_transaction(
        self,
        session: aiohttp.ClientSession,
        to_address: str,
        value_eth: float,
        gas_limit: Optional[int] = None,
        max_priority_fee: Optional[int] = None,
        max_fee: Optional[int] = None
    ) -> dict:
        """Create a transaction dictionary"""

        # Get current network conditions asynchronously
        nonce_task = AsyncWeb3Helper.get_transaction_count(session, self.rpc_url, self.account.address)
        chain_id_task = AsyncWeb3Helper.get_chain_id(session, self.rpc_url)
        block_task = AsyncWeb3Helper.get_block(session, self.rpc_url)

        nonce, chain_id, latest_block = await asyncio.gather(nonce_task, chain_id_task, block_task)

        # Get gas price estimates (EIP-1559)
        if max_fee is None or max_priority_fee is None:
            base_fee = int(latest_block['baseFeePerGas'], 16)

            if max_priority_fee is None:
                # Typical priority fee: 1-2 gwei
                max_priority_fee = self.w3.to_wei(2, 'gwei')

            if max_fee is None:
                # max_fee = base_fee * 2 + priority_fee (to handle base fee increases)
                max_fee = (base_fee * 2) + max_priority_fee

        if gas_limit is None:
            gas_limit = 21000  # Standard transfer

        # Build transaction
        transaction = {
            'type': 2,  # EIP-1559
            'chainId': chain_id,
            'from': self.account.address,
            'to': to_address,
            'value': self.w3.to_wei(value_eth, 'ether'),
            'gas': gas_limit,
            'maxFeePerGas': max_fee,
            'maxPriorityFeePerGas': max_priority_fee,
            'nonce': nonce,
        }

        logger.info("📝 Transaction Details:")
        logger.info(f"   From: {transaction['from']}")
        logger.info(f"   To: {transaction['to']}")
        logger.info(f"   Value: {value_eth} ETH")
        logger.info(f"   Gas Limit: {gas_limit}")
        logger.info(f"   Max Fee: {self.w3.from_wei(max_fee, 'gwei')} Gwei")
        logger.info(f"   Max Priority Fee: {self.w3.from_wei(max_priority_fee, 'gwei')} Gwei")
        logger.info(f"   Nonce: {nonce}")
        logger.info(f"   Chain ID: {chain_id}")

        return transaction

    def sign_transaction(self, transaction: dict) -> str:
        """Sign the transaction and return raw transaction hex"""
        signed_txn = self.account.sign_transaction(transaction)
        raw_transaction = signed_txn.raw_transaction.hex()
        logger.info(f"✅ Transaction signed")
        logger.info(f"   TX Hash: {signed_txn.hash.hex()}")
        return raw_transaction

    async def send_raw_transaction_sync(
        self,
        session: aiohttp.ClientSession,
        raw_transaction: str,
        timeout: Optional[int] = None
    ) -> Optional[Dict[str, Any]]:
        """
        Send raw transaction using eth_sendRawTransactionSync (EIP-7966)

        This method sends the transaction AND waits synchronously for the receipt.
        Returns the receipt directly without needing to poll.

        Args:
            session: aiohttp ClientSession
            raw_transaction: Signed transaction hex string
            timeout: Optional timeout in seconds (some implementations support this)

        Returns:
            Transaction receipt dict or None if failed
        """
        try:
            # Ensure raw transaction has 0x prefix
            if not raw_transaction.startswith('0x'):
                raw_transaction = '0x' + raw_transaction

            # Prepare params
            params = [raw_transaction]

            # Add timeout parameter if provided and supported
            if timeout is not None:
                params.append(timeout*1000)

            logger.info(f"📤 Sending transaction via eth_sendRawTransactionSync...")
            if timeout:
                logger.info(f"   Timeout: {timeout}s")

            # Send the async request
            result = await AsyncWeb3Helper.json_rpc_call(
                session,
                self.rpc_url,
                "eth_sendRawTransactionSync",
                params,
                timeout=(timeout + 10) if timeout else 120  # HTTP timeout slightly longer
            )

            # Check for JSON-RPC error
            if 'error' in result:
                error = result['error']
                logger.error(f"❌ RPC Error: {error.get('message', 'Unknown error')} Code: {error.get('code', 'N/A')}")
                if 'data' in error:
                    logger.error(f"   Data: {error['data']}")
                return None

            # Extract receipt from result
            receipt = result.get('result')
            if receipt is None:
                logger.error("❌ No receipt returned from eth_sendRawTransactionSync")
                return None

            logger.info(f"✅ Transaction confirmed!")
            logger.info(f"   TX Hash: {receipt.get('transactionHash', 'N/A')}")

            return receipt
        except asyncio.TimeoutError:
            logger.error(f"❌ Timeout: Request took longer than expected")
            logger.info("   The transaction may still be pending on the network")
            return None
        except aiohttp.ClientError as e:
            logger.error(f"❌ HTTP Request failed: {e}")
            return None
        except Exception as e:
            logger.error(f"❌ Failed to send transaction: {e}")
            import traceback
            traceback.print_exc()
            return None

    @staticmethod
    def print_receipt(receipt: dict):
        """Pretty print transaction receipt"""
        logger.info("=" * 70)
        logger.info("📄 TRANSACTION RECEIPT (from eth_sendRawTransactionSync)")
        logger.info("=" * 70)

        # Convert bytes to hex strings for JSON serialization
        receipt_clean = {}
        for key, value in receipt.items():
            if isinstance(value, bytes):
                receipt_clean[key] = value.hex()
            elif hasattr(value, 'hex'):
                receipt_clean[key] = value.hex()
            elif isinstance(value, list):
                # Handle logs array
                receipt_clean[key] = [
                    {k: v.hex() if isinstance(v, bytes) else v for k, v in item.items()}
                    if isinstance(item, dict) else item
                    for item in value
                ]
            else:
                receipt_clean[key] = value

        print(json.dumps(receipt_clean, indent=2, default=str))

        logger.info("=" * 70)
        logger.info("Key Information:")

        # Extract status (handle both int and hex formats)
        status = receipt.get('status')
        if isinstance(status, str):
            status = int(status, 16) if status.startswith('0x') else int(status)

        logger.info(f"   Status: {'✅ SUCCESS' if status == 1 else '❌ FAILED'}")

        # Extract block number (handle both int and hex formats)
        block_number = receipt.get('blockNumber')
        if isinstance(block_number, str) and block_number.startswith('0x'):
            block_number = int(block_number, 16)
        logger.info(f"   Block Number: {block_number}")

        # Extract gas used (handle both int and hex formats)
        gas_used = receipt.get('gasUsed')
        if isinstance(gas_used, str) and gas_used.startswith('0x'):
            gas_used = int(gas_used, 16)
        logger.info(f"   Gas Used: {gas_used}")

        # Extract effective gas price (handle both int and hex formats)
        effective_gas_price = receipt.get('effectiveGasPrice')
        if isinstance(effective_gas_price, str) and effective_gas_price.startswith('0x'):
            effective_gas_price = int(effective_gas_price, 16)

        if effective_gas_price:
            logger.info(f"   Effective Gas Price: {Web3.from_wei(effective_gas_price, 'gwei')} Gwei")
            logger.info(f"   Transaction Fee: {Web3.from_wei(gas_used * effective_gas_price, 'ether')} ETH")

        # L2-specific fields
        if 'l1Fee' in receipt:
            l1_fee = receipt['l1Fee']
            if isinstance(l1_fee, str) and l1_fee.startswith('0x'):
                l1_fee = int(l1_fee, 16)
            logger.info(f"   L1 Fee: {Web3.from_wei(l1_fee, 'ether')} ETH")

        logger.info("=" * 70)


async def test_method_support(session: aiohttp.ClientSession, rpc_url: str) -> bool:
    """Test if eth_sendRawTransactionSync is supported"""
    logger.info("\n🧪 Testing eth_sendRawTransactionSync support...")

    try:
        result = await AsyncWeb3Helper.json_rpc_call(
            session,
            rpc_url,
            "eth_sendRawTransactionSync",
            ["0x00"],  # Invalid transaction to test method existence
            timeout=10
        )

        if 'error' in result:
            error = result['error']
            if error.get('code') == -32601:
                logger.error("❌ eth_sendRawTransactionSync is NOT supported by this endpoint")
                logger.info("   Please use an endpoint that supports EIP-7966")
                return False
            else:
                logger.info(f"✅ Method exists (got error: {error.get('message')} - expected for invalid tx)")
                return True
        else:
            logger.info("✅ eth_sendRawTransactionSync appears to be supported")
            return True
    except Exception as e:
        logger.error(f"❌ Failed to test method: {e}")
        return False


async def check_connection(session: aiohttp.ClientSession, rpc_url: str) -> tuple[int, int]:
    """Check connection to Ethereum node"""
    logger.info(f"🔌 Connecting to Ethereum node...")

    try:
        chain_id_task = AsyncWeb3Helper.get_chain_id(session, rpc_url)
        block_number_task = AsyncWeb3Helper.get_block_number(session, rpc_url)

        chain_id, block_number = await asyncio.gather(chain_id_task, block_number_task)

        logger.info(f"✅ Connected to Ethereum node")
        logger.info(f"   Chain ID: {chain_id}")
        logger.info(f"   Latest Block: {block_number}")

        return chain_id, block_number
    except Exception as e:
        logger.error(f"❌ Failed to connect to Ethereum node URL: {rpc_url} error: {e}")
        raise


def get_explorer_url(chain_id: int, tx_hash: str) -> str:
    """Get the block explorer URL for a transaction"""
    explorers = {
        1: "https://etherscan.io/tx/",
        11155111: "https://sepolia.etherscan.io/tx/",
        8453: "https://basescan.org/tx/",
        42161: "https://arbiscan.io/tx/",
        10: "https://optimistic.etherscan.io/tx/",
    }
    base_url = explorers.get(chain_id, "https://etherscan.io/tx/")
    return f"{base_url}{tx_hash}"


async def main():
    """Main async function"""

    # Parse command-line arguments
    parser = argparse.ArgumentParser(
        description="Send an Ethereum transaction using eth_sendRawTransactionSync (EIP-7966)"
    )
    parser.add_argument(
        "--rpc-url",
        type=str,
        default="http://127.0.0.1:8545",  # Set the default value
        help="The HTTP URL of the Ethereum node (default: http://127.0.0.1:8545)",
    )
    parser.add_argument(
        "--private-key",
        type=str,
        help="Private key of sender wallet (without 0x prefix). If not provided, use --create-wallet.",
    )
    parser.add_argument(
        "--create-wallet",
        action="store_true",
        help="Create a new wallet and request funds from faucet",
    )
    parser.add_argument(
        "--to-address",
        type=str,
        help="Recipient address",
    )
    parser.add_argument(
        "--value",
        type=float,
        default=0.001,
        help="Amount to send in ETH (default: 0.001)",
    )
    parser.add_argument(
        "--gas-limit",
        type=int,
        default=21000,
        help="Gas limit (default: 21000)",
    )
    parser.add_argument(
        "--sync-timeout",
        type=int,
        default=60,
        help="Timeout for eth_sendRawTransactionSync in seconds (default: 60)",
    )
    parser.add_argument(
        "--wait-for-balance",
        action="store_true",
        help="Wait for wallet to receive balance before sending transaction",
    )
    parser.add_argument(
        "--test-method",
        action="store_true",
        help="Test if the RPC endpoint supports eth_sendRawTransactionSync",
    )

    args = parser.parse_args()

    # Create aiohttp session with connection pooling
    connector = aiohttp.TCPConnector(limit=10, limit_per_host=5)
    timeout = aiohttp.ClientTimeout(total=300)

    async with aiohttp.ClientSession(connector=connector, timeout=timeout) as session:
        try:
            # Check connection
            chain_id, _ = await check_connection(session, args.rpc_url)

            # Test if eth_sendRawTransactionSync is supported
            if args.test_method:
                supported = await test_method_support(session, args.rpc_url)
                if not supported:
                    return 1

                if not args.private_key and not args.create_wallet:
                    logger.info("\n✅ Test completed. Add --private-key or --create-wallet to send a real transaction.")
                    return 0

            # Handle wallet
            if args.create_wallet:
                # Create new wallet
                account = WalletManager.create_wallet()
                SepoliaFaucet.request_funds_manual(account.address)
                logger.info("\n⏸️  Please fund your wallet and run the script again with:")
                logger.info(f"   --private-key {account.key.hex()} --to-address {args.to_address}")
                return 0

            elif args.private_key:
                # Load existing wallet
                # Remove 0x prefix if present
                private_key = args.private_key
                if private_key.startswith('0x'):
                    private_key = private_key[2:]
                account = WalletManager.load_wallet(private_key)

            else:
                logger.error("❌ Please provide --private-key or use --create-wallet")
                return 1

            # Check balance asynchronously
            balance = await AsyncWeb3Helper.get_balance(session, args.rpc_url, account.address)
            balance_eth = Web3.from_wei(balance, 'ether')
            logger.info(f"💰 Current Balance: {balance_eth} ETH")

            if balance == 0:
                if args.wait_for_balance:
                    success = await SepoliaFaucet.wait_for_balance(
                        session, args.rpc_url, account.address
                    )
                    if not success:
                        return 1
                    balance = await AsyncWeb3Helper.get_balance(session, args.rpc_url, account.address)
                    balance_eth = Web3.from_wei(balance, 'ether')
                else:
                    logger.error("❌ Wallet has zero balance!")
                    SepoliaFaucet.request_funds_manual(account.address)
                    logger.info("\n💡 Tip: Use --wait-for-balance to automatically wait for funds")
                    return 1

            # Validate recipient address
            w3 = Web3()
            if not w3.is_address(args.to_address):
                logger.error(f"❌ Invalid recipient address: {args.to_address}")
                return 1

            to_address = w3.to_checksum_address(args.to_address)

            # Check if we have enough balance
            estimated_cost = w3.to_wei(args.value, 'ether') + (args.gas_limit * w3.to_wei(10, 'gwei'))
            if balance < estimated_cost:
                logger.error(f"❌ Insufficient balance!")
                logger.error(f"   Required: ~{w3.from_wei(estimated_cost, 'ether')} ETH")
                logger.error(f"   Available: {balance_eth} ETH")
                return 1

            # Create transaction sender
            tx_sender = AsyncTransactionSender(account, args.rpc_url)

            # Create transaction
            logger.info("\n" + "=" * 70)
            logger.info("CREATING TRANSACTION")
            logger.info("=" * 70)
            transaction = await tx_sender.create_transaction(
                session,
                to_address=to_address,
                value_eth=args.value,
                gas_limit=args.gas_limit
            )

            # Sign transaction
            logger.info("\n" + "=" * 70)
            logger.info("SIGNING TRANSACTION")
            logger.info("=" * 70)
            raw_transaction = tx_sender.sign_transaction(transaction)

            # Send transaction using eth_sendRawTransactionSync
            logger.info("\n" + "=" * 70)
            logger.info("SENDING TRANSACTION (eth_sendRawTransactionSync)")
            logger.info("=" * 70)
            logger.info("⏱️  This call will wait asynchronously for the receipt...")
            logger.info("   (No polling needed - EIP-7966 magic! ✨)")

            receipt = await tx_sender.send_raw_transaction_sync(
                session,
                raw_transaction,
                timeout=args.sync_timeout
            )

            if receipt is None:
                logger.error("❌ Failed to get transaction receipt from eth_sendRawTransactionSync")
                logger.info("   The transaction may still be pending or the method timed out")
                return 1

            # Print receipt
            AsyncTransactionSender.print_receipt(receipt)

            # Extract status (handle both int and hex formats)
            status = receipt.get('status')
            if isinstance(status, str):
                status = int(status, 16) if status.startswith('0x') else int(status)

            tx_hash = receipt.get('transactionHash', '')
            if isinstance(tx_hash, bytes):
                tx_hash = tx_hash.hex()

            if status == 1:
                logger.info("✅ Transaction completed successfully!")
                logger.info(f"   View on Explorer: {get_explorer_url(chain_id, tx_hash)}")
                return 0
            else:
                logger.error("❌ Transaction failed!")
                logger.info(f"   View on Explorer: {get_explorer_url(chain_id, tx_hash)}")
                return 1

        except KeyboardInterrupt:
            logger.info("⏸️  Interrupted by user")
            return 130
        except (ConnectionError, ClientConnectorError, ConnectionRefusedError):
            logger.error(f"🚨 Please check your node URL and try again!")
            return 1
        except Exception as e:
            logger.error(f"🚨 Unexpected error: {e.__class__.__name__}")
            import traceback
            traceback.print_exc()
            return 1


if __name__ == "__main__":
    """
    Usage Examples:

    1. Test if endpoint supports eth_sendRawTransactionSync:
       python eth_sendRawTransactionSync_async.py \\
           --rpc-url https://YOUR-QUICKNODE-URL.com \\
           --to-address 0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb \\
           --test-method

    2. Create a new wallet:
       python eth_sendRawTransactionSync_async.py \\
           --rpc-url https://YOUR-QUICKNODE-URL.com \\
           --create-wallet \\
           --to-address 0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb

    3. Send transaction with eth_sendRawTransactionSync (async):
       python eth_sendRawTransactionSync_async.py \\
           --rpc-url https://YOUR-QUICKNODE-URL.com \\
           --private-key YOUR_PRIVATE_KEY \\
           --to-address 0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb \\
           --value 0.001 \\
           --sync-timeout 60

    4. Use with custom timeout and wait for balance:
       python eth_sendRawTransactionSync_async.py \\
           --rpc-url https://YOUR-QUICKNODE-URL.com \\
           --private-key YOUR_PRIVATE_KEY \\
           --to-address 0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb \\
           --value 0.001 \\
           --sync-timeout 120 \\
           --wait-for-balance

    Note: This async version uses aiohttp for all HTTP requests and asyncio throughout.
    Perfect for high-throughput applications or integrating into async codebases.
    """
    exit_code = asyncio.run(main())
    sys.exit(exit_code)
