// +build integration

package registry

import (
	"database/sql"
	"testing"

	"go.uber.org/zap"
)

const testName = "test"
const testAddress = "127.0.0.1"

func TestRegister(t *testing.T) {
	r := beforeEach(t)
	upstream := newTestFixture(t)

	_, err := r.RegisterUpstream(upstream)
	if err != nil {
		t.Errorf("error registering upstream - %v", err)
	}

	db, err := sql.Open("mysql", "root@tcp(127.0.0.1:3306)/sshpiper")
	if err != nil {
		t.Errorf("error creating raw connection to mysql - %v", err)
	}
	defer db.Close()

	var name string
	var address string
	err = db.QueryRow("SELECT name, address FROM server LIMIT 1;").Scan(&name, &address)
	if err != nil {
		t.Errorf("error when querying database - %v", err)
	}
	if name != testName {
		t.Errorf("unexpeted name in server table - got %v", name)
	}

	if address != testAddress {
		t.Errorf("unexpcted address in server table")
	}

}
func TestUnregister(t *testing.T) {
	r := beforeEach(t)
	upstream := newTestFixture(t)

	_, err := r.RegisterUpstream(upstream)
	if err != nil {
		t.Errorf("error registering upstream - %v", err)
	}

	err = r.UnregisterUpstream(upstream)
	if err != nil {
		t.Errorf("error calling unregister - %v", err)
	}

	db, err := sql.Open("mysql", "root@tcp(127.0.0.1:3306)/sshpiper")
	if err != nil {
		t.Errorf("error creating raw connection to mysql - %v", err)
	}
	defer db.Close()

	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM server`).Scan(&count)
	if err != nil {
		t.Errorf("error when querying server table - %v", err)
	}
	if count != 0 {
		t.Errorf("expected table to be empty after unregistering - got %d", count)
	}

	err = db.QueryRow(`SELECT COUNT(*) FROM private_keys`).Scan(&count)
	if err != nil {
		t.Errorf("error when querying private_keys table - %v", err)
	}
	if count != 0 {
		t.Errorf("expected table to be empty after unregistering - got %d", count)
	}
}

func newTestFixture(t *testing.T) *Upstream {
	t.Helper()
	return &Upstream{
		Name:                testName,
		Username:            "fixture",
		Address:             testAddress,
		SSHPiperPrivateKey:  "any",
		DownstreamPublicKey: []string{"example"},
	}
}

func beforeEach(t *testing.T) *Registry {
	t.Helper()
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Error(err.Error())
	}

	r := NewRegistry(logger)
	err = r.ConnectDatabase()
	if err != nil {
		t.Errorf("error creating database connection %v", err)
	}

	return r
}
