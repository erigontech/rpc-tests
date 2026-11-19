package perf

import (
	"reflect"
	"testing"
)

func TestClientConnectionsSplitsTheConnectionsByShare(t *testing.T) {
	lines := []subscriptionLine{{typ: subNewHeads}, {typ: subNewHeads}, {typ: subLogs}, {typ: subLogs}, {typ: subReceipts}}
	clients := []ClientClass{
		{Name: "wallet", Share: 90, First: 0, Count: 1},
		{Name: "dapp", Share: 9, First: 1, Count: 3},
		{Name: "indexer", Share: 1, First: 4, Count: 1},
	}
	conns, err := clientConnections(100, lines, clients)
	if err != nil {
		t.Fatalf("clientConnections: %v", err)
	}
	count := map[string]int{}
	for _, c := range conns {
		count[c.class]++
	}
	if want := map[string]int{"wallet": 90, "dapp": 9, "indexer": 1}; !reflect.DeepEqual(count, want) {
		t.Errorf("connections by class %v, want %v", count, want)
	}
	if got := conns[0]; got.class != "wallet" || !reflect.DeepEqual(got.lines, []int{0}) {
		t.Errorf("first connection %+v: want wallet with line 0", got)
	}
	if got := conns[90]; got.class != "dapp" || !reflect.DeepEqual(got.lines, []int{1, 2, 3}) {
		t.Errorf("connection 90 %+v: want dapp with lines 1-3", got)
	}
	if got := conns[99]; got.class != "indexer" || !reflect.DeepEqual(got.lines, []int{4}) {
		t.Errorf("connection 99 %+v: want indexer with line 4", got)
	}
}

func TestClientConnectionsGivesEveryClassAConnection(t *testing.T) {
	lines := []subscriptionLine{{typ: subNewHeads}, {typ: subLogs}, {typ: subReceipts}}
	clients := []ClientClass{
		{Name: "wallet", Share: 90, First: 0, Count: 1},
		{Name: "dapp", Share: 9, First: 1, Count: 1},
		{Name: "indexer", Share: 1, First: 2, Count: 1},
	}
	conns, err := clientConnections(10, lines, clients)
	if err != nil {
		t.Fatalf("clientConnections: %v", err)
	}
	count := map[string]int{}
	for _, c := range conns {
		count[c.class]++
	}
	if want := map[string]int{"wallet": 8, "dapp": 1, "indexer": 1}; !reflect.DeepEqual(count, want) {
		t.Errorf("connections by class %v, want %v", count, want)
	}
	if _, err := clientConnections(2, lines, clients); err == nil {
		t.Error("2 connections for 3 classes: want an error")
	}
}

func TestClientConnectionsPicksTheLogsRequestsInTurn(t *testing.T) {
	lines := []subscriptionLine{{typ: subNewHeads}, {typ: subLogs}, {typ: subLogs}, {typ: subLogs}}
	clients := []ClientClass{{Name: "dapp", Share: 1, First: 0, Count: 4, Pick: 1}}
	conns, err := clientConnections(4, lines, clients)
	if err != nil {
		t.Fatalf("clientConnections: %v", err)
	}
	want := [][]int{{0, 1}, {0, 2}, {0, 3}, {0, 1}}
	for i, c := range conns {
		if !reflect.DeepEqual(c.lines, want[i]) {
			t.Errorf("connection %d lines %v, want %v", i, c.lines, want[i])
		}
	}
}

func TestClientConnectionsWithoutClassesSendsEveryLineOnEveryConnection(t *testing.T) {
	lines := []subscriptionLine{{typ: subNewHeads}, {typ: subLogs}}
	conns, err := clientConnections(3, lines, nil)
	if err != nil {
		t.Fatalf("clientConnections: %v", err)
	}
	for i, c := range conns {
		if c.class != "" || !reflect.DeepEqual(c.lines, []int{0, 1}) {
			t.Errorf("connection %d %+v: want every line, no class", i, c)
		}
	}
}

func TestClientConnectionsRejectsAClassOutsideTheRequests(t *testing.T) {
	lines := []subscriptionLine{{typ: subNewHeads}, {typ: subLogs}}
	for name, clients := range map[string][]ClientClass{
		"past the end":  {{Name: "a", Share: 1, First: 1, Count: 2}},
		"empty":         {{Name: "a", Share: 1, First: 0, Count: 0}},
		"no share":      {{Name: "a", Share: 0, First: 0, Count: 2}},
		"line left out": {{Name: "a", Share: 1, First: 0, Count: 1}},
	} {
		if _, err := clientConnections(4, lines, clients); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
