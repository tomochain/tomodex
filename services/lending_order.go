package services

import (
	"context"
	"encoding/json"
	"math"
	"math/big"
	"strconv"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/tomochain/tomox-sdk/errors"
	"github.com/tomochain/tomox-sdk/interfaces"
	"github.com/tomochain/tomox-sdk/rabbitmq"
	"github.com/tomochain/tomox-sdk/types"
	"github.com/tomochain/tomox-sdk/utils"
	"github.com/tomochain/tomox-sdk/ws"
)

const (
	LENDING_EVENT = "ORDER"
	REPAY_EVENT   = "REPAY"
	TOPUP_EVENT   = "TOPUP"
	RECALL_EVENT  = "RECALL"
)

// LendingOrderService struct
type LendingOrderService struct {
	lendingDao         interfaces.LendingOrderDao
	topupDao           interfaces.LendingOrderDao
	repayDao           interfaces.LendingOrderDao
	recallDao          interfaces.LendingOrderDao
	collateralTokenDao interfaces.TokenDao
	lendingTokenDao    interfaces.TokenDao
	notificationDao    interfaces.NotificationDao
	lendingTradeDao    interfaces.LendingTradeDao
	validator          interfaces.ValidatorService
	engine             interfaces.Engine
	broker             *rabbitmq.Connection
	mutext             sync.RWMutex
	bulkLendingOrders  map[string]map[common.Hash]*types.LendingOrder
}

// NewLendingOrderService returns a new instance of lending order service
func NewLendingOrderService(
	lendingDao interfaces.LendingOrderDao,
	topupDao interfaces.LendingOrderDao,
	repayDao interfaces.LendingOrderDao,
	recallDao interfaces.LendingOrderDao,
	collateralTokenDao interfaces.TokenDao,
	lendingTokenDao interfaces.TokenDao,
	notificationDao interfaces.NotificationDao,
	lendingTradeDao interfaces.LendingTradeDao,
	validator interfaces.ValidatorService,
	engine interfaces.Engine,
	broker *rabbitmq.Connection,
) *LendingOrderService {
	bulkLendingOrders := make(map[string]map[common.Hash]*types.LendingOrder)
	return &LendingOrderService{
		lendingDao,
		topupDao,
		repayDao,
		recallDao,
		collateralTokenDao,
		lendingTokenDao,
		notificationDao,
		lendingTradeDao,
		validator,
		engine,
		broker,
		sync.RWMutex{},
		bulkLendingOrders,
	}
}

// GetByHash get lending by hash
func (s *LendingOrderService) GetByHash(hash common.Hash) (*types.LendingOrder, error) {
	return s.lendingDao.GetByHash(hash)
}

// NewLendingOrder validates if the passed order is valid or not based on user's available
// funds and order data.
// If valid: LendingOrder is inserted in DB with order status as new and order is publiched
// on rabbitmq queue for matching engine to process the order
func (s *LendingOrderService) NewLendingOrder(o *types.LendingOrder) error {
	if err := o.Validate(); err != nil {
		logger.Error(err)
		return err
	}

	ok, err := o.VerifySignature()
	if err != nil {
		logger.Error(err)
	}

	if !ok {
		return errors.New("Invalid Signature")
	}

	if o.Type == types.TypeLimitOrder {
		err = s.validator.ValidateAvailablLendingBalance(o)
		if err != nil {
			logger.Error(err)
			return err
		}
	}

	err = s.broker.PublishLendingOrderMessage(o)
	if err != nil {
		logger.Error(err)
		return err
	}

	return nil
}

// CancelLendingOrder handles the cancellation order requests.
// Only Orders which are OPEN or NEW i.e. Not yet filled/partially filled
// can be cancelled
func (s *LendingOrderService) CancelLendingOrder(o *types.LendingOrder) error {
	return s.lendingDao.CancelLendingOrder(o)
}

// RepayLendingOrder repay
func (s *LendingOrderService) RepayLendingOrder(o *types.LendingOrder) error {
	return s.lendingDao.RepayLendingOrder(o)
}

// TopupLendingOrder topup
func (s *LendingOrderService) TopupLendingOrder(o *types.LendingOrder) error {

	return s.lendingDao.TopupLendingOrder(o)
}

// HandleLendingOrderResponse listens to messages incoming from the engine and handles websocket
// responses and database updates accordingly
func (s *LendingOrderService) HandleLendingOrderResponse(res *types.EngineResponse) error {
	switch res.Status {
	case types.LENDING_ORDER_ADDED:
		s.handleLendingOrderAdded(res)
		break
	case types.LENDING_ORDER_CANCELLED:
		s.handleLendingOrderCancelled(res)
		break
	case types.LENDING_ORDER_REJECTED:
		s.handleLendingOrderRejected(res)
		break
	case types.LENDING_ORDER_PARTIALLY_FILLED:
		s.handleLendingOrderPartialFilled(res)
		break
	case types.LENDING_ORDER_FILLED:
		s.handleLendingOrderFilled(res)
		break
	case types.LENDING_ORDER_REPAYED:
		s.handleLendingRepay(res)
		break
	case types.LENDING_ORDER_TOPUPED:
		s.handleLendingTopup(res)
		break
	case types.LENDING_ORDER_RECALLED:
		s.handleLendingRecall(res)
		break

	case types.LENDING_ORDER_TOPUP_REJECTED:
		s.handleLendingReject(res, types.LENDING_ORDER_TOPUP_REJECTED)
		break
	case types.LENDING_ORDER_REPAY_REJECTED:
		s.handleLendingReject(res, types.LENDING_ORDER_REPAY_REJECTED)
		break
	case types.LENDING_ORDER_RECALL_REJECTED:
		s.handleLendingReject(res, types.LENDING_ORDER_RECALL_REJECTED)
		break

	case types.ERROR_STATUS:
		s.handleEngineError(res)
		break
	default:
		s.handleEngineUnknownMessage(res)
	}

	if res.Status != types.ERROR_STATUS {
		err := s.saveBulkLendingOrders(res)
		if err != nil {
			logger.Error("Save bulk order", err)
		}
	}

	return nil
}

// handleLendingOrderAdded returns a websocket message informing the client that his order has been added
// to the orderbook (but currently not matched)
func (s *LendingOrderService) handleLendingOrderAdded(res *types.EngineResponse) {
	o := res.LendingOrder
	ws.SendLendingOrderMessage(types.LENDING_ORDER_ADDED, o.UserAddress, o)

	notifications, err := s.notificationDao.Create(&types.Notification{
		Recipient: o.UserAddress,
		Message: types.Message{
			MessageType: res.Status,
			Description: o.Hash.Hex(),
		},
		Type:   types.TypeLog,
		Status: types.StatusUnread,
	})

	if err != nil {
		logger.Error(err)
	}

	ws.SendNotificationMessage(types.SubscriptionEvent(res.Status), o.UserAddress, notifications)
	logger.Info("BroadcastOrderBookUpdate Lending Added")
}

func (s *LendingOrderService) handleLendingOrderPartialFilled(res *types.EngineResponse) {
}

func (s *LendingOrderService) handleLendingOrderFilled(res *types.EngineResponse) {
}

func (s *LendingOrderService) handleLendingTopup(res *types.EngineResponse) {
	o := res.LendingOrder

	notifications, err := s.notificationDao.Create(&types.Notification{
		Recipient: o.UserAddress,
		Message: types.Message{
			MessageType: res.Status,
			Description: o.Hash.Hex(),
		},
		Type:   types.TypeLog,
		Status: types.StatusUnread,
	})

	if err != nil {
		logger.Error(err)
	}

	ws.SendNotificationMessage(types.LENDING_ORDER_TOPUPED, o.UserAddress, notifications)
	lendingTrade, _ := s.lendingTradeDao.GetByHash(o.Hash)
	ws.SendLendingOrderMessage(types.LENDING_ORDER_TOPUPED, o.UserAddress, lendingTrade)
}

func (s *LendingOrderService) handleLendingRepay(res *types.EngineResponse) {
	o := res.LendingOrder

	notifications, err := s.notificationDao.Create(&types.Notification{
		Recipient: o.UserAddress,
		Message: types.Message{
			MessageType: res.Status,
			Description: o.Hash.Hex(),
		},
		Type:   types.TypeLog,
		Status: types.StatusUnread,
	})

	if err != nil {
		logger.Error(err)
	}

	ws.SendNotificationMessage(types.LENDING_ORDER_REPAYED, o.UserAddress, notifications)
	lendingTrade, _ := s.lendingTradeDao.GetByHash(o.Hash)
	ws.SendLendingOrderMessage(types.LENDING_ORDER_REPAYED, o.UserAddress, lendingTrade)
}

func (s *LendingOrderService) handleLendingRecall(res *types.EngineResponse) {
	o := res.LendingOrder
	ws.SendLendingOrderMessage(types.LENDING_ORDER_RECALLED, o.UserAddress, o)

	notifications, err := s.notificationDao.Create(&types.Notification{
		Recipient: o.UserAddress,
		Message: types.Message{
			MessageType: res.Status,
			Description: o.Hash.Hex(),
		},
		Type:   types.TypeLog,
		Status: types.StatusUnread,
	})

	if err != nil {
		logger.Error(err)
	}

	ws.SendNotificationMessage(types.LENDING_ORDER_RECALLED, o.UserAddress, notifications)
	lendingTrade, _ := s.lendingTradeDao.GetByHash(o.Hash)
	ws.SendLendingOrderMessage(types.LENDING_ORDER_RECALLED, o.UserAddress, lendingTrade)
}

func (s *LendingOrderService) handleLendingReject(res *types.EngineResponse, lendingType types.SubscriptionEvent) {
	o := res.LendingOrder
	ws.SendLendingOrderMessage(lendingType, o.UserAddress, o)

	notifications, err := s.notificationDao.Create(&types.Notification{
		Recipient: o.UserAddress,
		Message: types.Message{
			MessageType: res.Status,
			Description: o.Hash.Hex(),
		},
		Type:   types.TypeLog,
		Status: types.StatusUnread,
	})

	if err != nil {
		logger.Error(err)
	}

	ws.SendNotificationMessage(lendingType, o.UserAddress, notifications)
}

func (s *LendingOrderService) handleLendingOrderCancelled(res *types.EngineResponse) {
	o := res.LendingOrder

	notifications, err := s.notificationDao.Create(&types.Notification{
		Recipient: o.UserAddress,
		Message: types.Message{
			MessageType: "LENDING_ORDER_CANCELLED",
			Description: o.Hash.Hex(),
		},
		Type:   types.TypeLog,
		Status: types.StatusUnread,
	})

	if err != nil {
		logger.Error(err)
	}

	ws.SendLendingOrderMessage(types.LENDING_ORDER_CANCELLED, o.UserAddress, o)
	ws.SendNotificationMessage(types.LENDING_ORDER_CANCELLED, o.UserAddress, notifications)
	logger.Info("BroadcastOrderBookUpdate Lending Cancelled")
}

func (s *LendingOrderService) handleLendingOrderRejected(res *types.EngineResponse) {
}

// handleEngineError returns an websocket error message to the client and recovers orders on the
func (s *LendingOrderService) handleEngineError(res *types.EngineResponse) {
	o := res.LendingOrder

	notifications, err := s.notificationDao.Create(&types.Notification{
		Recipient: o.UserAddress,
		Message: types.Message{
			MessageType: types.LENDING_ORDER_REJECTED,
			Description: o.Hash.Hex(),
		},
		Type:   types.TypeLog,
		Status: types.StatusUnread,
	})

	if err != nil {
		logger.Error(err)
	}

	ws.SendLendingOrderMessage(types.LENDING_ORDER_REJECTED, o.UserAddress, o)
	ws.SendNotificationMessage(types.LENDING_ORDER_REJECTED, o.UserAddress, notifications)
	logger.Info("BroadcastOrderBookUpdate lending rejected")
}

// handleEngineUnknownMessage returns a websocket messsage in case the engine resonse is not recognized
func (s *LendingOrderService) handleEngineUnknownMessage(res *types.EngineResponse) {
}

// WatchChanges watch database
func (s *LendingOrderService) watchChanges(dao interfaces.LendingOrderDao, docType string) {
	ct, sc, err := dao.Watch()

	if err != nil {
		sc.Close()
		logger.Error("Failed to open change stream")
		return //exiting func
	}

	defer ct.Close()
	defer sc.Close()

	ctx := context.Background()
	//Handling change stream in a cycle
	for {
		select {
		case <-ctx.Done():
			logger.Error("Change stream closed")
			return
		default:
			ev := types.LendingOrderChangeEvent{}

			//getting next item from the steam
			ok := ct.Next(&ev)

			//if item from the stream un-marshaled successfully, do something with it
			if ok {
				logger.Debugf("Lending Operation Type: %s", ev.OperationType)
				s.HandleDocumentType(ev, docType)
			}
		}
	}
}

// WatchChanges watch database
func (s *LendingOrderService) WatchChanges() {
	go func() {
		for {
			<-time.After(500 * time.Millisecond)
			s.processBulkLendingOrders()
		}
	}()
	go s.watchChanges(s.lendingDao, LENDING_EVENT)
	go s.watchChanges(s.topupDao, TOPUP_EVENT)
	go s.watchChanges(s.repayDao, REPAY_EVENT)
	s.watchChanges(s.recallDao, RECALL_EVENT)
}

// HandleDocumentType handle order frome changing db
func (s *LendingOrderService) HandleDocumentType(ev types.LendingOrderChangeEvent, docType string) error {
	if ev.FullDocument == nil {
		return nil
	}
	res := &types.EngineResponse{}

	if ev.FullDocument.Status == types.LendingStatusOpen {
		res.Status = types.LENDING_ORDER_ADDED
		res.LendingOrder = ev.FullDocument
	} else if ev.FullDocument.Status == types.LendingStatusCancelled {
		res.Status = types.LENDING_ORDER_CANCELLED
		res.LendingOrder = ev.FullDocument
	} else if ev.FullDocument.Status == types.LendingStatusFilled {
		res.Status = types.LENDING_ORDER_FILLED
		res.LendingOrder = ev.FullDocument
	} else if ev.FullDocument.Status == types.LendingStatusPartialFilled {
		res.Status = types.LENDING_ORDER_PARTIALLY_FILLED
		res.LendingOrder = ev.FullDocument
	} else if ev.FullDocument.Status == types.LendingStatusRepay {
		res.Status = types.LENDING_ORDER_REPAYED
		res.LendingOrder = ev.FullDocument
	} else if ev.FullDocument.Status == types.LendingStatusTopup {
		res.Status = types.LENDING_ORDER_TOPUPED
		res.LendingOrder = ev.FullDocument
	} else if ev.FullDocument.Status == types.LendingStatusRecall {
		res.Status = types.LENDING_ORDER_RECALLED
		res.LendingOrder = ev.FullDocument
	} else if ev.FullDocument.Status == types.LendingStatusRejected {
		if docType == REPAY_EVENT {
			res.Status = types.LENDING_ORDER_REPAY_REJECTED
			res.LendingOrder = ev.FullDocument
		}
		if docType == TOPUP_EVENT {
			res.Status = types.LENDING_ORDER_TOPUP_REJECTED
			res.LendingOrder = ev.FullDocument
		}
		if docType == RECALL_EVENT {
			res.Status = types.LENDING_ORDER_RECALL_REJECTED
			res.LendingOrder = ev.FullDocument
		}
		if docType == LENDING_EVENT {
			res.Status = types.LENDING_ORDER_REJECTED
			res.LendingOrder = ev.FullDocument
		}

	}

	if res.Status != "" {
		err := s.broker.PublishLendingOrderResponse(res)
		if err != nil {
			logger.Error(err)
			return err
		}
	}

	return nil
}

// process for lending form rabbitmq

// HandleLendingOrdersCreateCancel handle lending order api
func (s *LendingOrderService) HandleLendingOrdersCreateCancel(msg *rabbitmq.Message) error {
	switch msg.Type {
	case "NEW_LENDING_ORDER":
		err := s.handleNewLendingOrder(msg.Data)
		if err != nil {
			logger.Error(err)
			return err
		}
	case "CANCEL_LENDING_ORDER":
		err := s.handleCancelLendingOrder(msg.Data)
		if err != nil {
			logger.Error(err)
			return err
		}
	default:
		logger.Error("Unknown message", msg)
	}

	return nil
}

func (s *LendingOrderService) handleNewLendingOrder(bytes []byte) error {
	o := &types.LendingOrder{}
	err := json.Unmarshal(bytes, o)
	if err != nil {
		logger.Error(err)
		return err
	}
	return s.lendingDao.AddNewLendingOrder(o)
}

func (s *LendingOrderService) handleCancelLendingOrder(bytes []byte) error {
	o := &types.LendingOrder{}
	err := json.Unmarshal(bytes, o)
	if err != nil {
		logger.Error(err)
		return err
	}
	return s.lendingDao.CancelLendingOrder(o)
}

// GetLendingNonceByUserAddress return nonce of user order
func (s *LendingOrderService) GetLendingNonceByUserAddress(addr common.Address) (uint64, error) {
	return s.lendingDao.GetLendingNonce(addr)
}

func (s *LendingOrderService) saveBulkLendingOrders(res *types.EngineResponse) error {
	id := utils.GetLendingOrderBookChannelID(res.LendingOrder.Term, res.LendingOrder.LendingToken)

	s.mutext.Lock()
	defer s.mutext.Unlock()

	if _, ok := s.bulkLendingOrders[id]; ok {
		s.bulkLendingOrders[id][res.LendingOrder.Hash] = res.LendingOrder
	} else {
		s.bulkLendingOrders[id] = make(map[common.Hash]*types.LendingOrder)
		s.bulkLendingOrders[id][res.LendingOrder.Hash] = res.LendingOrder
	}
	return nil
}

func (s *LendingOrderService) processBulkLendingOrders() {
	s.mutext.Lock()
	defer s.mutext.Unlock()
	for p, orders := range s.bulkLendingOrders {
		borrow := []map[string]string{}
		lend := []map[string]string{}

		if len(orders) <= 0 {
			continue
		}
		for _, o := range orders {
			side := o.Side
			amount, err := s.lendingDao.GetLendingOrderBookInterest(o.Term, o.LendingToken, o.Interest, side)
			if err != nil {
				logger.Error(err)
			}

			// case where the amount at the pricepoint is equal to 0
			if amount == nil {
				amount = big.NewInt(0)
			}

			update := map[string]string{
				"interest": strconv.FormatUint(o.Interest, 10),
				"amount":   amount.String(),
			}

			if side == types.BORROW {
				borrow = append(borrow, update)
			} else {
				lend = append(lend, update)
			}
		}
		ws.GetLendingOrderBookSocket().BroadcastMessage(p, &types.LendingOrderBook{
			Name:   p,
			Borrow: borrow,
			Lend:   lend,
		})
	}
	s.bulkLendingOrders = make(map[string]map[common.Hash]*types.LendingOrder)
}

// GetLendingOrders filter lending
func (s *LendingOrderService) GetLendingOrders(lendingSpec types.LendingSpec, sort []string, offset int, size int) (*types.LendingRes, error) {
	return s.lendingDao.GetLendingOrders(lendingSpec, sort, offset, size)
}

// GetTopup filter topup
func (s *LendingOrderService) GetTopup(topupSpec types.TopupSpec, sort []string, offset int, size int) (*types.LendingRes, error) {
	lendingSpec := types.LendingSpec{
		UserAddress:     topupSpec.UserAddress,
		CollateralToken: topupSpec.CollateralToken,
		LendingToken:    topupSpec.LendingToken,
		Term:            topupSpec.Term,
		DateFrom:        topupSpec.DateFrom,
		DateTo:          topupSpec.DateTo,
	}
	return s.topupDao.GetLendingOrders(lendingSpec, sort, offset, size)
}

// GetRepay filter repay
func (s *LendingOrderService) GetRepay(repaySpec types.RepaySpec, sort []string, offset int, size int) (*types.LendingRes, error) {
	lendingSpec := types.LendingSpec{
		UserAddress:  repaySpec.UserAddress,
		LendingToken: repaySpec.LendingToken,
		Term:         repaySpec.Term,
		DateFrom:     repaySpec.DateFrom,
		DateTo:       repaySpec.DateTo,
	}
	return s.repayDao.GetLendingOrders(lendingSpec, sort, offset, size)
}

// GetRecall filter recall
func (s *LendingOrderService) GetRecall(recallSpec types.RecallSpec, sort []string, offset int, size int) (*types.LendingRes, error) {
	lendingSpec := types.LendingSpec{
		UserAddress:     recallSpec.UserAddress,
		CollateralToken: recallSpec.CollateralToken,
		LendingToken:    recallSpec.LendingToken,
		Term:            recallSpec.Term,
		DateFrom:        recallSpec.DateFrom,
		DateTo:          recallSpec.DateTo,
	}
	return s.recallDao.GetLendingOrders(lendingSpec, sort, offset, size)
}

// EstimateCollateral estimate collateral amount to make lending
func (s *LendingOrderService) EstimateCollateral(collateralToken common.Address, lendingToken common.Address, lendingAmount *big.Float) (*big.Float, *big.Float, error) {
	lendingTokenInfo, err := s.lendingTokenDao.GetByAddress(lendingToken)
	if err != nil {
		return nil, nil, err
	}
	collateralTokenInfo, err := s.collateralTokenDao.GetByAddress(collateralToken)
	if err != nil {
		return nil, nil, err
	}
	collateralPrice, err := s.lendingDao.GetLastTokenPrice(collateralToken, lendingToken, collateralTokenInfo.Decimals, lendingTokenInfo.Decimals)
	if err != nil {
		return nil, nil, err
	}

	lendingDecimals := big.NewInt(int64(math.Pow10(lendingTokenInfo.Decimals)))
	x := new(big.Float).Quo(new(big.Float).SetInt(collateralPrice), new(big.Float).SetInt(lendingDecimals))
	a := new(big.Float).Mul(lendingAmount, new(big.Float).SetInt(lendingDecimals))
	collateralAmount := new(big.Float).Quo(a, new(big.Float).SetInt(collateralPrice))
	return collateralAmount, x, nil
}
