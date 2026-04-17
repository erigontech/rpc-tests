
# Disabled tests for Ethereum mainnet
DISABLED_TEST_LIST=(
  net_listening/test_1.json
  engine_
  # these tests requires Erigon active
  admin_nodeInfo/test_01.json
  admin_peers/test_01.json
  erigon_nodeInfo/test_1.json
  eth_coinbase/test_01.json
  eth_createAccessList/test_16.json
  eth_getTransactionByHash/test_02.json
  # Small prune issue that leads to wrong ReceiptDomain data at 16999999 (probably at every million) block: https://github.com/erigontech/erigon/issues/13050
  ots_searchTransactionsBefore/test_04.tar
  eth_getWork/test_01.json
  eth_mining/test_01.json
  eth_protocolVersion/test_1.json
  eth_submitHashrate/test_1.json
  eth_submitWork/test_1.json
  net_peerCount/test_1.json
  net_version/test_1.json
  txpool_status/test_1.json
  web3_clientVersion/test_1.json

  # these tests require commitment enabled
  eth_getBlockReceipts/test_01.json
  eth_getProof/test_21.json
  eth_getProof/test_22.json
  eth_getProof/test_23.json
  eth_getProof/test_24.json
  eth_getProof/test_25.json
  eth_getProof/test_26.json
  eth_simulateV1/test_01.json
  eth_simulateV1/test_02.json
  eth_simulateV1/test_03.json
  eth_simulateV1/test_08.json
  eth_simulateV1/test_09.json
  eth_simulateV1/test_10.json
  eth_simulateV1/test_11.json
  eth_simulateV1/test_17.json
  eth_simulateV1/test_19.json
  eth_simulateV1/test_21.json
  eth_simulateV1/test_22.json
  eth_simulateV1/test_23.json
  eth_simulateV1/test_24.json
  eth_simulateV1/test_26.json
  eth_simulateV1/test_28.json
)


# Transform the array into a comma-separated string
DISABLED_TESTS=$(IFS=,; echo "${DISABLED_TEST_LIST[*]}")

./run_tests.py -f -c -x $DISABLED_TESTS $*

