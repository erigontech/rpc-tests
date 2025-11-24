"""MPT root for Ethereum receipts"""

import hexbytes
import rlp
import web3.types

from . import mpt


class ReceiptValue(mpt.TrieValue):
    """Value type for an Ethereum transaction receipt"""
    receipt: web3.types.TxReceipt

    def __init__(self, receipt: web3.types.TxReceipt):
        self.receipt = receipt

    def encode(self):
        """Construct the RLP-serializable receipt list (the structure depends on Byzantium vs. pre-Byzantium fork)"""

        # Convert receipt logs from AttributeDict to the required list format
        logs_list = []
        for log in self.receipt['logs']:
            logs_list.append([
                hexbytes.HexBytes(log['address']),  # address
                [hexbytes.HexBytes(topic) for topic in log['topics']],  # list of topics
                hexbytes.HexBytes(log['data'])  # data
            ])

        # Create the RLP-serializable list
        if 'status' in self.receipt:
            # Post-Byzantium: [status, cumulativeGasUsed, logsBloom, logs]
            value_list = [
                self.receipt['status'],
                self.receipt['cumulativeGasUsed'],
                hexbytes.HexBytes(self.receipt['logsBloom']),
                logs_list
            ]
        elif 'root' in self.receipt:
            # Pre-Byzantium: [stateRoot, cumulativeGasUsed, logsBloom, logs]
            value_list = [
                self.receipt['root'],  # stateRoot
                self.receipt['cumulativeGasUsed'],
                hexbytes.HexBytes(self.receipt['logsBloom']),
                logs_list
            ]
        else:
            raise ValueError(f"Receipt for block {self.receipt['blockNumber']} has neither 'status' nor 'root' field.")

        # RLP-encode the value and insert it into the trie
        value = rlp.encode(value_list)

        # Receipt type must be included *only* for non-legacy txn type
        receipt_type = self.receipt['type']
        if receipt_type != 0:
            value = self.receipt['type'].to_bytes(length=1, byteorder='big') + value

        return value


def compute_receipts_root(receipts: web3.types.BlockReceipts) -> bytes:
    return mpt.compute_trie_root([ReceiptValue(receipt) for receipt in receipts])
