package vegetagen

// EthCall turns a transaction into an eth_call with the same call arguments.
type EthCall struct{}

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
	txn := it.Txn
	args := callArgs{From: txn.From, To: txn.To, Gas: txn.Gas, GasPrice: txn.GasPrice, Value: txn.Value}
	if txn.Input != "0x" {
		args.Data = txn.Input
	}
	return marshalRequest("eth_call", args, p.BlockParam)
}
