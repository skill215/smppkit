package smpp

import (
	"errors"
	"time"
)

// Config bounds one connection generation. Zero values use the documented
// defaults. An inbound request consumer must respond before InboundTimeout;
// notifications use a separate bounded queue and have no ACK timer.
type Config struct {
	Window            int           // 16 outbound requests, including queued writes
	InboundLimit      int           // 32 outstanding requests and queued responses
	NotificationLimit int           // 32 queued inbound notifications and 32 concurrent outbound Notify calls
	RequestTimeout    time.Duration // 30 seconds, includes Request/Notify admission
	InboundTimeout    time.Duration // 30 seconds, closes unanswered sessions
	ReadTimeout       time.Duration // 2 minutes, includes partial frame acquisition
	WriteTimeout      time.Duration // 10 seconds per complete frame
}

func (c Config) normalized() (Config, error) {
	if c.Window == 0 {
		c.Window = 16
	}
	if c.InboundLimit == 0 {
		c.InboundLimit = 32
	}
	if c.NotificationLimit == 0 {
		c.NotificationLimit = 32
	}
	if c.RequestTimeout == 0 {
		c.RequestTimeout = 30 * time.Second
	}
	if c.InboundTimeout == 0 {
		c.InboundTimeout = 30 * time.Second
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = 2 * time.Minute
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = 10 * time.Second
	}
	if c.Window < 1 || c.Window > 65536 || c.InboundLimit < 1 || c.InboundLimit > 65536 || c.NotificationLimit < 1 || c.NotificationLimit > 65536 || c.RequestTimeout < 0 || c.InboundTimeout < 0 || c.ReadTimeout < 0 || c.WriteTimeout < 0 {
		return c, errors.New("smpp: invalid session limits")
	}
	return c, nil
}
