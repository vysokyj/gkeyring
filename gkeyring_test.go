package gkeyring

import (
	"testing"
)


func TestCRUD(t *testing.T) {
	const (
		service = "testservice"
		user = "testuser"
		pass = "testpass"
	)

	Set(service, user, pass)
	pass2, err := Get(service,user)
	if err != nil {
		t.Error(err)
	}
	if pass2 != pass {
		t.Error("Pass not match!")
	}
	err = Delete(service, user)
	if err != nil {
		t.Error(err)
	}
}

func TestList(t *testing.T) {
	List()
}