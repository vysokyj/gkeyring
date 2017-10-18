package gkeyring

import (
	"fmt"

	"github.com/godbus/dbus"
)

const (
	serviceName         = "org.freedesktop.secrets"
	servicePath         = "/org/freedesktop/secrets"
	serviceInterface    = "org.freedesktop.Secret.Service"
	collectionInterface = "org.freedesktop.Secret.Collection"
	itemInterface       = "org.freedesktop.Secret.Item"
	sessionInterface    = "org.freedesktop.Secret.Session"
	promptInterface     = "org.freedesktop.Secret.Prompt"

	collectionBasePath = "/org/freedesktop/secrets/collection/"
)

type secret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string `dbus:"content_type"`
}

func newSecret(session dbus.ObjectPath, sec string) secret {
	return secret{
		Session:     session,
		Parameters:  []byte{},
		Value:       []byte(sec),
		ContentType: "text/plain; charset=utf8",
	}
}

// secretService is an interface for the Secret Service dbus API.
type secretService struct {
	*dbus.Conn
	object dbus.BusObject
}


var (
	// ErrNotFound is the expected error if the secret isn't found in the
	// keyring.
	ErrNotFound = fmt.Errorf("secret not found in keyring")
)


// NewSecretService initializes a new secretService object.
func newSecretService() (*secretService, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, err
	}

	return &secretService{
		conn,
		conn.Object(serviceName, servicePath),
	}, nil
}



// GetCollection returns a collection from a name.
func (s *secretService) getCollection(name string) dbus.BusObject {
	return s.Object(serviceName, dbus.ObjectPath(collectionBasePath+name))
}

// Unlock unlocks a collection.
func (s *secretService) unlock(collection dbus.ObjectPath) error {
	var unlocked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	err := s.object.Call(serviceInterface+".Unlock", 0, []dbus.ObjectPath{collection}).Store(&unlocked, &prompt)
	if err != nil {
		return err
	}

	_, v, err := s.handlePrompt(prompt)
	if err != nil {
		return err
	}

	collections := v.Value()
	switch c := collections.(type) {
	case []dbus.ObjectPath:
		unlocked = append(unlocked, c...)
	}

	if len(unlocked) != 1 || unlocked[0] != collection {
		return fmt.Errorf("failed to unlock correct collection '%v'", collection)
	}

	return nil
}


// CreateCollection with the supplied label.
func (s *secretService) createCollection(label string) (dbus.BusObject, error) {
	properties := map[string]dbus.Variant{
		collectionInterface + ".Label": dbus.MakeVariant(label),
	}
	var collection, prompt dbus.ObjectPath
	err := s.object.Call(serviceInterface+".CreateCollection", 0, properties, "").
		Store(&collection, &prompt)
	if err != nil {
		return nil, err
	}

	_, v, err := s.handlePrompt(prompt)
	if err != nil {
		return nil, err
	}

	if v.String() != "" {
		collection = dbus.ObjectPath(v.String())
	}

	return s.Object(serviceName, collection), nil
}

// CreateItem creates an item in a collection, with label, attributes and a
// related secret.
func (s *secretService) createItem(collection dbus.BusObject, label string, attributes map[string]string, secret secret) error {
	properties := map[string]dbus.Variant{
		itemInterface + ".Label":      dbus.MakeVariant(label),
		itemInterface + ".Attributes": dbus.MakeVariant(attributes),
	}

	var item, prompt dbus.ObjectPath
	err := collection.Call(collectionInterface+".CreateItem", 0,
		properties, secret, true).Store(&item, &prompt)
	if err != nil {
		return err
	}

	_, _, err = s.handlePrompt(prompt)
	if err != nil {
		return err
	}

	return nil
}

// handlePrompt checks if a prompt should be handles and handles it by
// triggering the prompt and waiting for the Sercret service daemon to display
// the prompt to the user.
func (s *secretService) handlePrompt(prompt dbus.ObjectPath) (bool, dbus.Variant, error) {
	if prompt != dbus.ObjectPath("/") {
		err := s.Object(serviceName, prompt).Call(promptInterface+".Prompt", 0, "").Err
		if err != nil {
			return false, dbus.MakeVariant(""), err
		}

		promptSignal := make(chan *dbus.Signal, 1)
		s.Signal(promptSignal)

		signal := <-promptSignal
		switch signal.Name {
		case promptInterface + ".Completed":
			dismissed := signal.Body[0].(bool)
			result := signal.Body[1].(dbus.Variant)
			return dismissed, result, nil
		}

	}

	return false, dbus.MakeVariant(""), nil
}


// SearchItems returns a list of items matching the search object.
func (s *secretService) searchItems(collection dbus.BusObject, search interface{}) ([]dbus.ObjectPath, error) {
	var results []dbus.ObjectPath
	err := collection.Call(collectionInterface+".SearchItems", 0, search).Store(&results)
	if err != nil {
		return nil, err
	}

	return results, nil
}

// GetSecret gets secret from an item in a given session.
func (s *secretService) getSecret(itemPath dbus.ObjectPath, session dbus.ObjectPath) (*secret, error) {
	var secret secret
	err := s.Object(serviceName, itemPath).Call(itemInterface+".GetSecret", 0, session).Store(&secret)
	if err != nil {
		return nil, err
	}

	return &secret, nil
}



func (s *secretService) deleteItem(itemPath dbus.ObjectPath) error {
	var prompt dbus.ObjectPath
	err := s.Object(serviceName, itemPath).Call(itemInterface+".Delete", 0).Store(&prompt)
	if err != nil {
		return err
	}

	_, _, err = s.handlePrompt(prompt)
	if err != nil {
		return err
	}

	return nil
}


func (s *secretService) openSession() (dbus.BusObject, error) {
	var disregard dbus.Variant
	var sessionPath dbus.ObjectPath
	err := s.object.Call(serviceInterface+".OpenSession", 0, "plain", dbus.MakeVariant("")).Store(&disregard, &sessionPath)
	if err != nil {
		return nil, err
	}

	return s.Object(serviceName, sessionPath), nil
}

func (s *secretService) closeSession(session dbus.BusObject) error {
	return session.Call(sessionInterface+".Close", 0).Err
}



// findItem look up an item by service and user.
func (s *secretService) findItem(service, user string) (dbus.ObjectPath, error) {
	collection := s.getCollection("login")

	search := map[string]string{
		"username": user,
		"service":  service,
	}

	err := s.unlock(collection.Path())
	if err != nil {
		return "", err
	}

	results, err := s.searchItems(collection, search)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "", ErrNotFound
	}

	return results[0], nil
}

// ---------------------------------------------------------------------------------------------------------------------
// PUBLIC API


// Set stores user and pass in the keyring under the defined service name.
func Set(service, user, pass string) error {
	attributes := map[string]string{
		"username": user,
		"service":  service,
	}

	s, err := newSecretService()
	if err != nil {
		return err
	}

	// open a session
	session, err := s.openSession()
	if err != nil {
		return err
	}
	defer s.closeSession(session)

	secret := newSecret(session.Path(), pass)

	collection := s.getCollection("login")

	err = s.unlock(collection.Path())
	if err != nil {
		return err
	}

	err = s.createItem(collection,
		fmt.Sprintf("Password for '%s' on '%s'", user, service),
		attributes, secret)
	if err != nil {
		return err
	}

	return nil
}

// Get gets a secret from the keyring given a service name and a user.
func Get(service, user string) (string, error) {
	s, err := newSecretService()
	if err != nil {
		return "", err
	}


	item, err := s.findItem(service, user)
	if err != nil {
		return "", err
	}

	// open a session
	session, err := s.openSession()
	if err != nil {
		return "", err
	}
	defer s.closeSession(session)

	secret, err := s.getSecret(item, session.Path())
	if err != nil {
		return "", err
	}

	return string(secret.Value), nil
}

// Delete deletes a secret, identified by service & user, from the keyring.
func Delete(service, user string) error {
	s, err := newSecretService()
	if err != nil {
		return err
	}

	item, err := s.findItem(service, user)
	if err != nil {
		return err
	}

	return s.deleteItem(item)
}

// List all secret items
func List() (map[string]string, error) {
	s, err := newSecretService()
	if err != nil {
		return nil, err
	}


	collection := s.getCollection("login")
	err = s.unlock(collection.Path())
	if err != nil {
		return nil, err
	}

	//s.listItems(collection)


	//for _, item := range items {
	//	secret, err := svc.GetSecret(item, session.Path())
	//	if err != nil {
	//		return nil, err
	//	}
	//	log.Println(secret)
	//
	//}


	return nil, nil

}


