package perf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
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

func TestSplitCPUsGivesAQuarterToTheLightConnections(t *testing.T) {
	for _, tt := range []struct {
		cpus, light, heavy []int
	}{
		{cpus: []int{24, 25, 26, 27, 28, 29, 30, 31}, light: []int{24, 25}, heavy: []int{26, 27, 28, 29, 30, 31}},
		{cpus: []int{0, 1}, light: []int{0}, heavy: []int{1}},
		{cpus: []int{3}},
		{},
	} {
		light, heavy := splitCPUs(tt.cpus)
		if !reflect.DeepEqual(light, tt.light) || !reflect.DeepEqual(heavy, tt.heavy) {
			t.Errorf("splitCPUs(%v) = %v, %v, want %v, %v", tt.cpus, light, heavy, tt.light, tt.heavy)
		}
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

	conns, _, _, err := collect(context.Background(), "ws://"+addr, reqs, lines, plan, time.Second)
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

	if _, _, _, err := collect(ctx, "ws://"+addr, reqs, lines, plan, time.Minute); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("collect: %v, want the deadline of the test", err)
	}
}
