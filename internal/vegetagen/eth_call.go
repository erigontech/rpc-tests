package vegetagen

// EthCall turns a transaction into an eth_call with the same call arguments.
type EthCall struct{}

// EstimateGas turns a transaction into an eth_estimateGas with the same call arguments.
type EstimateGas struct{}

type callArgs struct {
	From     string  `json:"from"`
	To       *string `json:"to,omitempty"`
	Gas      string  `json:"gas,omitempty"`
	GasPrice string  `json:"gasPrice,omitempty"`
	Value    string  `json:"value,omitempty"`
	Data     string  `json:"data,omitempty"`
}

func (EthCall) Method() string { return "eth_call" }
func (EthCall) Kind() Kind     { return PerTx }

func (EthCall) Request(it Item, p Params) ([]byte, error) {
	return marshalRequest("eth_call", txCallArgs(it.Txn), p.BlockParam)
}

func (EstimateGas) Method() string { return "eth_estimateGas" }
func (EstimateGas) Kind() Kind     { return PerTx }

// Request keeps the tx gas: it caps the node's binary search, and the tx fit within it.
func (EstimateGas) Request(it Item, p Params) ([]byte, error) {
	return marshalRequest("eth_estimateGas", txCallArgs(it.Txn), p.BlockParam)
}

func txCallArgs(txn *Transaction) callArgs {
	args := callArgs{From: txn.From, To: txn.To, Gas: txn.Gas, GasPrice: txn.GasPrice, Value: txn.Value}
	if txn.Input != "0x" {
		args.Data = txn.Input
	}
	return args
}
