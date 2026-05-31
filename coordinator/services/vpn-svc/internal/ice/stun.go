package ice

import (
	"fmt"
	"log/slog"
	"net"
)

// STUNServer implements RFC 5389 STUN (Session Traversal Utilities for NAT).
// It runs on a UDP port and responds to STUN BINDING REQUEST messages
// with the external IP:port of the requester.
type STUNServer struct {
	addr   *net.UDPAddr
	conn   *net.UDPConn
	logger *slog.Logger
}

// NewSTUNServer creates a new STUN server listening on the given UDP address.
func NewSTUNServer(listenAddr string, logger *slog.Logger) (*STUNServer, error) {
	addr, err := net.ResolveUDPAddr("udp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve addr: %w", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen udp: %w", err)
	}

	return &STUNServer{
		addr:   addr,
		conn:   conn,
		logger: logger,
	}, nil
}

// Start begins accepting STUN requests (blocks until Close is called).
func (s *STUNServer) Start() error {
	s.logger.Info("stun server started", slog.String("addr", s.addr.String()))
	defer s.conn.Close()

	buffer := make([]byte, 1024)
	for {
		n, remoteAddr, err := s.conn.ReadFromUDP(buffer)
		if err != nil {
			s.logger.Error("read error", slog.String("error", err.Error()))
			continue
		}

		// Parse STUN message
		msg, err := ParseSTUNMessage(buffer[:n])
		if err != nil {
			s.logger.Warn("invalid stun message", slog.String("error", err.Error()))
			continue
		}

		// Only handle BINDING REQUEST
		if msg.MessageType != MessageTypeBindingRequest {
			continue
		}

		// Send BINDING SUCCESS response with MAPPED-ADDRESS = sender IP:port
		response := &STUNMessage{
			MessageType: MessageTypeBindingSuccess,
			TransactionID: msg.TransactionID,
			Attributes: []*STUNAttribute{
				{
					Type:  AttributeMappedAddress,
					Value: &XORMappedAddress{
						Family: net.IPv4len,
						Port:   uint16(remoteAddr.Port),
						IP:     remoteAddr.IP,
					},
				},
			},
		}

		responseBytes := response.Encode()
		_, err = s.conn.WriteToUDP(responseBytes, remoteAddr)
		if err != nil {
			s.logger.Error("write error", slog.String("error", err.Error()))
			continue
		}

		s.logger.Debug("stun binding response sent",
			slog.String("remote_addr", remoteAddr.String()),
		)
	}
}

// Close shuts down the STUN server.
func (s *STUNServer) Close() error {
	return s.conn.Close()
}

// STUN message type constants (RFC 5389 §6)
const (
	MessageTypeBindingRequest  = 0x0001
	MessageTypeBindingSuccess  = 0x0101
	MessageTypeBindingError    = 0x0111
)

// STUN attribute type constants (RFC 5389 §15)
const (
	AttributeMappedAddress     = 0x0001
	AttributeXORMappedAddress  = 0x0020
	AttributeUsername          = 0x0006
	AttributeMessageIntegrity  = 0x0008
	AttributeFingerprint       = 0x8028
)

// STUNMessage represents a STUN message
type STUNMessage struct {
	MessageType   uint16
	MessageLength uint16
	MagicCookie   uint32
	TransactionID [12]byte
	Attributes    []*STUNAttribute
}

// STUNAttribute represents a STUN attribute
type STUNAttribute struct {
	Type  uint16
	Value interface{} // XORMappedAddress, []byte (username), etc.
}

// XORMappedAddress represents XOR-MAPPED-ADDRESS attribute
type XORMappedAddress struct {
	Family uint8  // 0x01 = IPv4, 0x02 = IPv6
	Port   uint16
	IP     net.IP
}

// ParseSTUNMessage parses a STUN message from bytes
func ParseSTUNMessage(data []byte) (*STUNMessage, error) {
	if len(data) < 20 {
		return nil, fmt.Errorf("stun message too short")
	}

	// Parse fixed header (20 bytes)
	msgType := (uint16(data[0]) << 8) | uint16(data[1])
	msgLen := (uint16(data[2]) << 8) | uint16(data[3])
	magicCookie := (uint32(data[4]) << 24) | (uint32(data[5]) << 16) |
		(uint32(data[6]) << 8) | uint32(data[7])
	transID := [12]byte{}
	copy(transID[:], data[8:20])

	// Validate magic cookie (RFC 5389 §6)
	const stunMagicCookie = 0x2112A442
	if magicCookie != stunMagicCookie {
		return nil, fmt.Errorf("invalid magic cookie")
	}

	msg := &STUNMessage{
		MessageType: msgType,
		MessageLength: msgLen,
		MagicCookie: magicCookie,
		TransactionID: transID,
	}

	// Parse attributes if present
	// (For now, we ignore attributes in the request)
	return msg, nil
}

// Encode encodes a STUN message to bytes
func (m *STUNMessage) Encode() []byte {
	// Encode attributes first to calculate length
	attrBytes := []byte{}
	for _, attr := range m.Attributes {
		attrBytes = append(attrBytes, encodeAttribute(attr)...)
	}

	// Encode fixed header (20 bytes) + attributes
	result := make([]byte, 0, 20+len(attrBytes))

	// Message type (2 bytes)
	result = append(result,
		byte(m.MessageType >> 8),
		byte(m.MessageType & 0xFF),
	)

	// Message length = attributes length (2 bytes)
	result = append(result,
		byte(len(attrBytes) >> 8),
		byte(len(attrBytes) & 0xFF),
	)

	// Magic cookie (4 bytes)
	const stunMagicCookie = 0x2112A442
	result = append(result,
		byte(stunMagicCookie >> 24),
		byte((stunMagicCookie >> 16) & 0xFF),
		byte((stunMagicCookie >> 8) & 0xFF),
		byte(stunMagicCookie & 0xFF),
	)

	// Transaction ID (12 bytes)
	result = append(result, m.TransactionID[:]...)

	// Attributes
	result = append(result, attrBytes...)

	return result
}

// encodeAttribute encodes a single STUN attribute
func encodeAttribute(attr *STUNAttribute) []byte {
	result := []byte{}

	switch v := attr.Value.(type) {
	case *XORMappedAddress:
		// XOR-MAPPED-ADDRESS: family (1) + port (2) + IP (4 or 16)
		attrValue := []byte{0x00, v.Family}

		// XOR port with high 16 bits of magic cookie
		xorPort := v.Port ^ 0x2112
		attrValue = append(attrValue, byte(xorPort >> 8), byte(xorPort & 0xFF))

		// XOR IP with magic cookie (and transaction ID for IPv6)
		const stunMagicCookie = 0x2112A442
		for i, b := range v.IP {
			if i < 4 {
				attrValue = append(attrValue, b ^ byte((stunMagicCookie >> (24 - (i * 8))) & 0xFF))
			}
		}

		// Attribute header: type + length + value
		valueLen := len(attrValue)
		result = append(result,
			byte(AttributeXORMappedAddress >> 8),
			byte(AttributeXORMappedAddress & 0xFF),
			byte(valueLen >> 8),
			byte(valueLen & 0xFF),
		)
		result = append(result, attrValue...)
	}

	return result
}
