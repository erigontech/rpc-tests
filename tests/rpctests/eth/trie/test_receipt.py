""" Tests for receipt MPT computation utilities """

import hexbytes
import unittest
import web3.types

from rpctests.eth.trie.mpt import EMPTY_TRIE_ROOT
from rpctests.eth.trie.receipt import compute_receipts_root

# ETH transfer receipt structure: only [status, cumulativeGasUsed, logsBloom, logs] matter for receipt MPT computation
# Note: Status is b'\x01' for success. 0 is b'\x80'
ETH_TRANSFER_RECEIPT: web3.types.TxReceipt = {
    'type': 0,
    'status': 1,  # Status: Success
    'cumulativeGasUsed': 21000,  # Cumulative Gas Used
    'logsBloom': b'\x00' * 256,  # 256-byte empty bloom filter
    'logs': [],  # Logs (empty list)
    # Fields below DO NOT MATTER
    'blockHash': hexbytes.HexBytes(''),
    'blockNumber': 0,
    'gasUsed': 0,
    'contractAddress': None,
    'effectiveGasPrice': 0,
    'root': None,
    'from': None,
    'to': None,
    'transactionHash': hexbytes.HexBytes(''),
    'transactionIndex': 0
}


class TestComputeReceiptsRoot(unittest.TestCase):
    """Unit tests for the compute_receipts_root function."""

    def test_empty_receipts_root(self):
        """Tests the root for the empty list of receipts (block with no transactions)."""
        receipts = []
        calculated_root = compute_receipts_root(receipts)

        self.assertEqual(calculated_root, EMPTY_TRIE_ROOT)

    def test_single_eth_transfer_receipt_root(self):
        """Tests the root for the simplest possible list of receipts: a successful ETH transfer."""
        receipt_list = [ETH_TRANSFER_RECEIPT]
        calculated_root = compute_receipts_root(receipt_list)

        self.assertEqual('056b23fbba480696b65fe5a59b8f2148a1299103c4f57df839233af2cf4ca2d2', calculated_root.hex())

    def test_double_eth_transfer_receipt_root(self):
        """Tests the root for a pair of successful ETH transfers."""
        receipt_list = [ETH_TRANSFER_RECEIPT, ETH_TRANSFER_RECEIPT]
        calculated_root = compute_receipts_root(receipt_list)

        self.assertEqual('d85d19b0b39baeea9bd64f1ca2415d68bf8298d6319af2fda74cd98ec43eadcc', calculated_root.hex())

    def test_single_token_transfer_receipt_root(self):
        """Tests the root for a successful USDT token transfer."""
        usdt_token_transfer_receipt: web3.types.TxReceipt = {
            'type': 2,
            'status': 1,  # Status: Success
            'cumulativeGasUsed': 0xf6e9,  # Cumulative Gas Used
            'logsBloom': '0000000000000010000000000000000000000000000000000000000000000000'
                         '0000000000000000000000000000010000000000000000000000000000000000'
                         '0000000000000000000000080000000000000000000000000000000000000000'
                         '0002000000020000000000000000000000000000000000002000001000000000'
                         '0000000000000000000000400000000000000000000000000000000000100000'
                         '0000000000000000000040800000000000000000000000000000000000000000'
                         '0000000200000000000000000000000000000000000000000000000000000000'
                         '0000000000000000000000000000000000000000000000000000000000000000',
            'logs': [{
                'address': '0xdac17f958d2ee523a2206206994597c13d831ec7',
                "topics": ['0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef',
                           '0x000000000000000000000000e4829f89bdc892895e0500a1b12b75b526e8e5c0',
                           '0x000000000000000000000000c7ecb7803ab2de1c6bcff2062fae196f94785e45'],
                'data': '0x000000000000000000000000000000000000000000000000000000004a486738',
            }],
            # Fields below DO NOT MATTER
            'blockHash': hexbytes.HexBytes(''),
            'blockNumber': 0,
            'gasUsed': 0,
            'contractAddress': None,
            'effectiveGasPrice': 0,
            'root': None,
            'from': None,
            'to': None,
            'transactionHash': hexbytes.HexBytes(''),
            'transactionIndex': 0,
        }
        receipt_list = [usdt_token_transfer_receipt]
        calculated_root = compute_receipts_root(receipt_list)

        self.assertEqual('7e2ac4a575f7c6c77a862f66bb38ae70a2f7193b93b9a9f8d5458c133ee10e69', calculated_root.hex())


if __name__ == "__main__":
    unittest.main()
