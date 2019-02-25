package ws

import (
	"github.com/tomochain/dex-server/errors"
	"github.com/tomochain/dex-server/types"
)

var priceBoardSocket *PriceBoardSocket

// PriceBoardSocket holds the map of subscriptions subscribed to price board channels
// corresponding to the key/event they have subscribed to.
type PriceBoardSocket struct {
	subscriptions     map[string]map[*Client]bool
	subscriptionsList map[*Client][]string
}

func NewPriceBoardSocket() *PriceBoardSocket {
	return &PriceBoardSocket{
		subscriptions:     make(map[string]map[*Client]bool),
		subscriptionsList: make(map[*Client][]string),
	}
}

// GetPriceBoardSocket return singleton instance of PriceBoardSocket type struct
func GetPriceBoardSocket() *PriceBoardSocket {
	if priceBoardSocket == nil {
		priceBoardSocket = NewPriceBoardSocket()
	}

	return priceBoardSocket
}

// Subscribe handles the subscription of connection to get
// streaming data over the socker for any pair.
func (s *PriceBoardSocket) Subscribe(channelID string, c *Client) error {
	if c == nil {
		return errors.New("No connection found")
	}

	if s.subscriptions[channelID] == nil {
		s.subscriptions[channelID] = make(map[*Client]bool)
	}

	s.subscriptions[channelID][c] = true

	if s.subscriptionsList[c] == nil {
		s.subscriptionsList[c] = []string{}
	}

	s.subscriptionsList[c] = append(s.subscriptionsList[c], channelID)

	return nil
}

// UnsubscribeHandler returns function of type unsubscribe handler,
// it handles the unsubscription of pair in case of connection closing.
func (s *PriceBoardSocket) UnsubscribeHandler(channelID string) func(c *Client) {
	return func(c *Client) {
		s.UnsubscribeChannel(channelID, c)
	}
}

// Unsubscribe is used to unsubscribe the connection from listening to the key subscribed to.
// It can be called on unsubscription message from user or due to some other reason by system
func (s *PriceBoardSocket) UnsubscribeChannel(channelID string, c *Client) {
	if s.subscriptions[channelID][c] {
		s.subscriptions[channelID][c] = false
		delete(s.subscriptions[channelID], c)
	}
}

func (s *PriceBoardSocket) Unsubscribe(c *Client) {
	channelIds := s.subscriptionsList[c]
	if channelIds == nil {
		return
	}

	for _, id := range s.subscriptionsList[c] {
		s.UnsubscribeChannel(id, c)
	}
}

// BroadcastMessage streams message to all the subscriptions subscribed to the pair
func (s *PriceBoardSocket) BroadcastMessage(channelID string, p interface{}) error {

	for c, status := range s.subscriptions[channelID] {
		if status {
			s.SendUpdateMessage(c, p)
		}
	}

	return nil
}

// SendMessage sends a websocket message on the price board channel
func (s *PriceBoardSocket) SendMessage(c *Client, msgType types.SubscriptionEvent, p interface{}) {
	c.SendMessage(PriceBoardChannel, msgType, p)
}

// SendInitMessage sends INIT message on price board channel on subscription event
func (s *PriceBoardSocket) SendInitMessage(c *Client, data interface{}) {
	c.SendMessage(PriceBoardChannel, types.INIT, data)
}

// SendUpdateMessage sends UPDATE message on price board channel as new data is created
func (s *PriceBoardSocket) SendUpdateMessage(c *Client, data interface{}) {
	c.SendMessage(PriceBoardChannel, types.UPDATE, data)
}

// SendErrorMessage sends error message on price board channel
func (s *PriceBoardSocket) SendErrorMessage(c *Client, data interface{}) {
	c.SendMessage(PriceBoardChannel, types.ERROR, data)
}
