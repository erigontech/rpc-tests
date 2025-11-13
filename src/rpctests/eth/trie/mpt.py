"""Merkle-Patricia Trie (MPT) computation for Ethereum entity values"""

import rlp
import trie

from abc import ABC, abstractmethod
from typing import List

# This is the standard root hash for an empty MPT
EMPTY_TRIE_ROOT = trie.HexaryTrie.BLANK_NODE_HASH


class TrieValue(ABC):
    """Value type for the MPT used to verify the cryptographic integrity of a list of entity values."""
    @abstractmethod
    def encode(self):
        """Return the specific RLP-encoded representation for this value type"""
        pass


def compute_trie_root(values: List[TrieValue]):
    """Compute the root of the modified Merkle-Patricia Trie (MPT) built upon the specified values."""
    # Create a new empty MPT
    mpt = trie.HexaryTrie(db={})

    # Iterate over the value elements, encode them and add them to the trie
    for index, value in enumerate(values):
        key = rlp.encode(index, sedes=rlp.sedes.big_endian_int)
        mpt[key] = value.encode()

    return mpt.root_hash
