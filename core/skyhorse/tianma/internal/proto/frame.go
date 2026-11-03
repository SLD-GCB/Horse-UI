// Package proto is the Tianma wire format: Hysteria-style HTTP/3 auth plus a
// TCP request frame carried on a raw QUIC stream.
package proto

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"

	"github.com/apernet/quic-go/quicvarint"
)

const (
	// URLHost and URLPath identify the auth request among normal web traffic.
	URLHost = "tianma"
	URLPath = "/auth"

	// HeaderAuth carries "<uuid>:<hex token>".
	HeaderAuth = "Tianma-Auth"
	// HeaderPadding carries random bytes to blur the request size.
	HeaderPadding = "Tianma-Padding"

	// StatusAuthOK is the non-standard status Hysteria uses for auth success.
	StatusAuthOK = 233

	// FrameTypeTCPRequest is the QUIC frame type that starts a TCP relay.
	FrameTypeTCPRequest = 0x401
)

// Addr is a target address.
type Addr struct {
	Host string
	Port uint16
}

func (a Addr) String() string {
	return net.JoinHostPort(a.Host, strconv.Itoa(int(a.Port)))
}

// ParseAddr converts "host:port" into an Addr.
func ParseAddr(target string) (Addr, error) {
	host, portText, err := net.SplitHostPort(target)
	if err != nil {
		return Addr{}, err
	}
	if host == "" {
		return Addr{}, errors.New("tianma: empty host")
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		return Addr{}, err
	}
	return Addr{Host: host, Port: uint16(port)}, nil
}

// WriteTCPRequest writes frame type, address and padding.
func WriteTCPRequest(w io.Writer, address Addr, padding int) error {
	encoded := []byte(address.String())
	buffer := quicvarint.Append(nil, FrameTypeTCPRequest)
	buffer = quicvarint.Append(buffer, uint64(len(encoded)))
	buffer = append(buffer, encoded...)
	buffer = quicvarint.Append(buffer, uint64(padding))
	buffer = append(buffer, make([]byte, padding)...)
	_, err := w.Write(buffer)
	return err
}

// ReadTCPRequest reads the address and padding after the frame type.
func ReadTCPRequest(r io.Reader) (Addr, error) {
	reader := bufio.NewReader(r)
	length, err := quicvarint.Read(reader)
	if err != nil {
		return Addr{}, err
	}
	if length == 0 || length > 1024 {
		return Addr{}, fmt.Errorf("tianma: bad address length %d", length)
	}
	encoded := make([]byte, length)
	if _, err := io.ReadFull(reader, encoded); err != nil {
		return Addr{}, err
	}
	padding, err := quicvarint.Read(reader)
	if err != nil {
		return Addr{}, err
	}
	if padding > 0 {
		if _, err := io.CopyN(io.Discard, reader, int64(padding)); err != nil {
			return Addr{}, err
		}
	}
	return ParseAddr(string(encoded))
}

// EncodeToken formats the auth header value.
func EncodeToken(uuid string, token []byte) string {
	return uuid + ":" + hex.EncodeToString(token)
}

// DecodeToken parses the auth header value.
func DecodeToken(value string) (string, []byte, error) {
	for index := 0; index < len(value); index++ {
		if value[index] != ':' {
			continue
		}
		token, err := hex.DecodeString(value[index+1:])
		if err != nil {
			return "", nil, err
		}
		return value[:index], token, nil
	}
	return "", nil, errors.New("tianma: malformed auth header")
}
