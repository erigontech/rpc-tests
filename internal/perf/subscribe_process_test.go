package perf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv(SubscribeWorkerEnv) != "" {
		if err := RunSubscribeWorker(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestHeavyConnectionsHoldReceiptsOrLogsWithoutFilter(t *testing.T) {
	lines := []subscriptionLine{
		{typ: subNewHeads},
		{typ: subLogs, filter: map[string]any{"address": "0x01"}},
		{typ: subLogs, filter: map[string]any{}},
		{typ: subReceipts},
	}
	plan := []clientConnection{{lines: []int{0}}, {lines: []int{0, 1}}, {lines: []int{2}}, {lines: []int{0, 3}}}
	if got, want := heavyConnections(plan, lines), []bool{false, false, true, true}; !reflect.DeepEqual(got, want) {
		t.Errorf("heavy %v, want %v", got, want)
	}
}

func TestCollectReadsTheHeavyConnectionsInAChildProcess(t *testing.T) {
	node := &wsNode{conns: 4, total: 4, linesOf: func(string) int { return 1 }, heads: headsWithTxs(2, 3)}
	addr := startWSNode(t, node)
	reqs := [][]byte{[]byte(newHeadsRequest), []byte(receiptsRequest)}
	lines, err := subscriptionLines(reqs)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := clientConnections(4, lines, []ClientClass{{Name: "wallet", Share: 3, First: 0, Count: 1}, {Name: "indexer", Share: 1, First: 1, Count: 1}})
	if err != nil {
		t.Fatal(err)
	}

	conns, _, _, err := collect(context.Background(), "ws://"+addr, reqs, lines, plan, 1, time.Second)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	for i, c := range conns {
		child := c.pid != os.Getpid()
		if child != (c.class == "indexer") {
			t.Errorf("connection %d of class %s read by pid %d, test pid %d: want only the indexer in a child process", i, c.class, c.pid, os.Getpid())
		}
		if len(c.notes) != 2 {
			t.Errorf("connection %d of class %s: %d notifications, want 2", i, c.class, len(c.notes))
		}
	}
}

func TestCollectWithAChildProcessReturnsTheCancelOfTheTest(t *testing.T) {
	node := &wsNode{conns: 2, total: 2, linesOf: func(string) int { return 1 }, heads: headsWithTxs(1, 1)}
	addr := startWSNode(t, node)
	reqs := [][]byte{[]byte(newHeadsRequest), []byte(receiptsRequest)}
	lines, err := subscriptionLines(reqs)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := clientConnections(2, lines, []ClientClass{{Name: "wallet", Share: 1, First: 0, Count: 1}, {Name: "indexer", Share: 1, First: 1, Count: 1}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if _, _, _, err := collect(ctx, "ws://"+addr, reqs, lines, plan, 1, time.Minute); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("collect: %v, want the deadline of the test", err)
	}
}

func TestCollectSplitSubscribesTheConnectionsOfBothProcessesInTheGivenOrder(t *testing.T) {
	node := &wsNode{conns: 4, total: 4, linesOf: func(string) int { return 1 }, heads: headsWithTxs(1, 1)}
	addr := startWSNode(t, node)
	reqs := [][]byte{[]byte(newHeadsRequest), []byte(receiptsRequest)}
	lines, err := subscriptionLines(reqs)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := clientConnections(4, lines, []ClientClass{{Name: "wallet", Share: 1, First: 0, Count: 1}, {Name: "indexer", Share: 1, First: 1, Count: 1}})
	if err != nil {
		t.Fatal(err)
	}
	received := make([]connection, len(plan))
	for i, p := range plan {
		received[i].class, received[i].lines = p.class, p.lines
	}
	var order []int // an indexer, a wallet, the other indexer, the other wallet
	for _, class := range []string{"indexer", "wallet", "indexer", "wallet"} {
		for i, p := range plan {
			if p.class == class && !slices.Contains(order, i) {
				order = append(order, i)
				break
			}
		}
	}

	if _, _, err := collectSplit(context.Background(), "ws://"+addr, reqs, lines, received, heavyConnections(plan, lines), order, 100*time.Millisecond); err != nil {
		t.Fatalf("collectSplit: %v", err)
	}
	got := make([]string, len(plan))
	for conn := range plan {
		got[conn] = node.types[conn][0]
	}
	if want := []string{"transactionReceipts", "newHeads", "transactionReceipts", "newHeads"}; !reflect.DeepEqual(got, want) {
		t.Errorf("subscriptions reached the node in the order %v, want %v", got, want)
	}
}

func TestSubscribeOrderDependsOnlyOnTheSeed(t *testing.T) {
	if a, b := subscribeOrder(500, 7), subscribeOrder(500, 7); !reflect.DeepEqual(a, b) {
		t.Error("the same seed gave two orders")
	}
	if a, b := subscribeOrder(500, 7), subscribeOrder(500, 8); reflect.DeepEqual(a, b) {
		t.Error("two seeds gave the same order")
	}
}
