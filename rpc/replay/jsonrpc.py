""" Player for JSON RPC API replay """

class JsonRpc:
    """ makes for JSON-RPC requests """

    @staticmethod
    def create_get_block_by_number(block: str):
        """ create JSONRPC getBlockByNumber
        """
        return "{\"jsonrpc\":\"2.0\", \"method\":\"eth_getBlockByNumber\", \"params\":[" + "\""+str(block) + "\", true], \"id\":1}"

    @staticmethod
    def create_trace_transaction(txn_hash: str):
        """ create JSONRPC trace_replayTransaction
        """
        return "{\"jsonrpc\":\"2.0\", \"method\":\"trace_replayTransaction\", \"params\":[" + "\"" + str(txn_hash) + "\", [ \"vmTrace\" ] ], \"id\":1}"

    @staticmethod
    def create_debug_trace_transaction(txn_hash: str):
        """ create JSONRPC debug_replayTransaction
        """
        return "{\"jsonrpc\":\"2.0\", \"method\":\"debug_traceTransaction\", \"params\":[" + "\"" + str(txn_hash) + "\", { \"disableMemory\": false,  \"disableStack\": false, \"disableStorage\": false } ], \"id\":1}"
