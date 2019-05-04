package services

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/globalsign/mgo/bson"
	"github.com/tomochain/tomodex/interfaces"
	"github.com/tomochain/tomodex/types"
)

// NotificationService struct with daos required, responsible for communicating with dao
// NotificationService functions are responsible for interacting with dao and implements business logic.
type NotificationService struct {
	NotificationDao interfaces.NotificationDao
}

// NewNotificationService returns a new instance of NewNotificationService
func NewNotificationService(
	notificationDao interfaces.NotificationDao,
) *NotificationService {
	return &NotificationService{
		NotificationDao: notificationDao,
	}
}

// Create inserts a new token into the database
func (s *NotificationService) Create(n *types.Notification) error {
	err := s.NotificationDao.Create(n)

	if err != nil {
		logger.Error(err)
		return err
	}

	return nil
}

// GetAll fetches all the notifications from db
func (s *NotificationService) GetAll() ([]types.Notification, error) {
	return s.NotificationDao.GetAll()
}

// GetByUserAddress fetches all the notifications related to user address
func (s *NotificationService) GetByUserAddress(addr common.Address, limit ...int) ([]*types.Notification, error) {
	return s.NotificationDao.GetByUserAddress(addr, limit...)
}

// GetByID fetches the detailed document of a notification using its mongo ID
func (s *NotificationService) GetByID(id bson.ObjectId) (*types.Notification, error) {
	return s.NotificationDao.GetByID(id)
}