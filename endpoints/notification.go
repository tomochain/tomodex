package endpoints

import (
	"encoding/json"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/mux"
	"github.com/tomochain/tomodex/interfaces"
	"github.com/tomochain/tomodex/types"
	"github.com/tomochain/tomodex/ws"
)

type NotificationEndpoint struct {
	NotificationService interfaces.NotificationService
}

// ServeNotificationResource sets up the routing of notification endpoints and the corresponding handlers.
func ServeNotificationResource(
	r *mux.Router,
	notificationService interfaces.NotificationService,
) {
	e := &NotificationEndpoint{notificationService}

	ws.RegisterChannel(ws.NotificationChannel, e.handleNotificationWebSocket)
}

func (e *NotificationEndpoint) handleNotificationWebSocket(input interface{}, c *ws.Client) {
	b, _ := json.Marshal(input)
	var ev *types.WebsocketEvent

	err := json.Unmarshal(b, &ev)
	if err != nil {
		logger.Error(err)
	}

	if ev.Type != types.SUBSCRIBE {
		logger.Info("Event Type", ev.Type)
		err := map[string]string{"Message": "Invalid payload"}
		ws.SendNotificationErrorMessage(c, err)
		return
	}

	if ev.Type == types.SUBSCRIBE {
		b, _ = json.Marshal(ev.Payload)
		var addr string

		err = json.Unmarshal(b, &addr)
		if err != nil {
			logger.Error(err)
			ws.SendNotificationErrorMessage(c, err)
			return
		}

		if !common.IsHexAddress(addr) {
			err := map[string]string{"Message": "Invalid address"}
			ws.SendNotificationErrorMessage(c, err)
			return
		}

		a := common.HexToAddress(addr)

		ws.RegisterNotificationConnection(a, c)
		notifications, err := e.NotificationService.GetByUserAddress(a)

		if err != nil {
			logger.Error(err)
			ws.SendNotificationErrorMessage(c, err)
			return
		}

		ws.SendNotificationMessage(types.INIT, a, notifications)
	}
}