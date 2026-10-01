package client

import "testing"

func TestActiveUpdateStreamReattachClosedClientIsIntentional(t *testing.T) {
	client := &Client{}
	if client.IsClosed() {
		t.Fatal("new client is closed")
	}
	client.Close()
	if !client.IsClosed() {
		t.Fatal("closed client would be retried as an unexpected disconnect")
	}
}
