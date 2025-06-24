package notifier

import (
	"encoding/json"
	"fmt"
	"time"

	"xqueue/internal/config"
	"xqueue/internal/models"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

// MQTTNotifier MQTT通知器
type MQTTNotifier struct {
	client mqtt.Client
	config *config.MQTTConfig
	logger *logrus.Logger
}

// NewMQTTNotifier 创建MQTT通知器
func NewMQTTNotifier(cfg *config.MQTTConfig, logger *logrus.Logger) (*MQTTNotifier, error) {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(cfg.Broker)
	opts.SetClientID(cfg.ClientID)

	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
	}
	if cfg.Password != "" {
		opts.SetPassword(cfg.Password)
	}

	opts.SetAutoReconnect(true)
	opts.SetConnectTimeout(time.Second * 10)
	opts.SetKeepAlive(time.Second * 30)
	opts.SetPingTimeout(time.Second * 10)

	// 设置连接丢失处理器
	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		logger.WithError(err).Error("MQTT connection lost")
	})

	// 设置连接成功处理器
	opts.SetOnConnectHandler(func(client mqtt.Client) {
		logger.Info("MQTT connected successfully")
	})

	client := mqtt.NewClient(opts)

	notifier := &MQTTNotifier{
		client: client,
		config: cfg,
		logger: logger,
	}

	return notifier, nil
}

// Connect 连接到MQTT broker
func (m *MQTTNotifier) Connect() error {
	token := m.client.Connect()
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %w", token.Error())
	}

	m.logger.Info("MQTT notifier connected")
	return nil
}

// Disconnect 断开MQTT连接
func (m *MQTTNotifier) Disconnect() {
	if m.client.IsConnected() {
		m.client.Disconnect(250)
		m.logger.Info("MQTT notifier disconnected")
	}
}

// NotifyTaskStatus 推送任务状态变更
func (m *MQTTNotifier) NotifyTaskStatus(taskID string, status models.TaskStatus, errorMsg string) error {
	if !m.client.IsConnected() {
		return fmt.Errorf("MQTT client is not connected")
	}

	message := models.TaskMessage{
		TaskID:    taskID,
		Status:    status,
		Error:     errorMsg,
		Timestamp: time.Now(),
	}

	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal task message: %w", err)
	}

	// 发布到通用状态topic
	topic := fmt.Sprintf("%s/%s", m.config.Topic, taskID)
	token := m.client.Publish(topic, m.config.QoS, false, payload)

	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to publish task status: %w", token.Error())
	}

	// 发布到状态特定的topic
	statusTopic := fmt.Sprintf("%s/status/%s", m.config.Topic, status)
	token = m.client.Publish(statusTopic, m.config.QoS, false, payload)

	if token.Wait() && token.Error() != nil {
		m.logger.WithError(token.Error()).Warn("Failed to publish to status topic")
	}

	m.logger.WithFields(logrus.Fields{
		"task_id": taskID,
		"status":  status,
		"topic":   topic,
	}).Debug("Task status notification sent")

	return nil
}

// NotifyTaskCreated 推送任务创建通知
func (m *MQTTNotifier) NotifyTaskCreated(task *models.Task) error {
	return m.NotifyTaskStatus(task.ID, task.Status, "")
}

// NotifyTaskProgress 推送任务进度通知
func (m *MQTTNotifier) NotifyTaskProgress(taskID string, progress map[string]interface{}) error {
	if !m.client.IsConnected() {
		return fmt.Errorf("MQTT client is not connected")
	}

	message := map[string]interface{}{
		"task_id":   taskID,
		"progress":  progress,
		"timestamp": time.Now(),
	}

	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal progress message: %w", err)
	}

	topic := fmt.Sprintf("%s/progress/%s", m.config.Topic, taskID)
	token := m.client.Publish(topic, m.config.QoS, false, payload)

	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to publish task progress: %w", token.Error())
	}

	m.logger.WithFields(logrus.Fields{
		"task_id": taskID,
		"topic":   topic,
	}).Debug("Task progress notification sent")

	return nil
}

// Subscribe 订阅指定topic
func (m *MQTTNotifier) Subscribe(topic string, handler mqtt.MessageHandler) error {
	if !m.client.IsConnected() {
		return fmt.Errorf("MQTT client is not connected")
	}

	token := m.client.Subscribe(topic, m.config.QoS, handler)
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to subscribe to topic %s: %w", topic, token.Error())
	}

	m.logger.WithField("topic", topic).Info("Subscribed to MQTT topic")
	return nil
}

// Unsubscribe 取消订阅指定topic
func (m *MQTTNotifier) Unsubscribe(topic string) error {
	if !m.client.IsConnected() {
		return fmt.Errorf("MQTT client is not connected")
	}

	token := m.client.Unsubscribe(topic)
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to unsubscribe from topic %s: %w", topic, token.Error())
	}

	m.logger.WithField("topic", topic).Info("Unsubscribed from MQTT topic")
	return nil
}

// IsConnected 检查连接状态
func (m *MQTTNotifier) IsConnected() bool {
	return m.client.IsConnected()
}
