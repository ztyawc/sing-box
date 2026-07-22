package socks

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/netip"

	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/common/varbin"
	S "github.com/sagernet/sing/protocol/socks"
	"github.com/sagernet/sing/protocol/socks/socks5"
)

type privateAuthMethod byte

const (
	privateAuthMethod80       privateAuthMethod = 0x80
	privateAuthMethod82       privateAuthMethod = 0x82
	privateAuthUsernameLength                   = 19
)

var privateAuth82FixedData = [...]byte{
	0x14, 0x01, 0x01, 0x01, 0x02, 0x04, 0x00, 0x00, 0x00, 0x00,
	0x03, 0x02, 0x27, 0x10, 0x04, 0x01, 0x01, 0x05, 0x02, 0x00, 0x04,
}

func parsePrivateAuthMethod(method string) (privateAuthMethod, error) {
	switch method {
	case "":
		return 0, nil
	case "0x80":
		return privateAuthMethod80, nil
	case "0x82":
		return privateAuthMethod82, nil
	default:
		return 0, E.New("unknown private socks authentication method: ", method)
	}
}

func validatePrivateAuthOptions(version S.Version, method privateAuthMethod, username string, password string) error {
	if method == 0 {
		return nil
	}
	if version != S.Version5 {
		return E.New("private socks authentication requires SOCKS5")
	}
	if len(username) != privateAuthUsernameLength {
		return E.New("private socks username must be exactly ", privateAuthUsernameLength, " bytes")
	}
	if password == "" {
		return E.New("private socks password must not be empty")
	}
	return nil
}

var _ N.Dialer = (*privateClient)(nil)

type privateClient struct {
	dialer     N.Dialer
	serverAddr M.Socksaddr
	method     privateAuthMethod
	username   string
	password   string
}

func newPrivateClient(dialer N.Dialer, serverAddr M.Socksaddr, method privateAuthMethod, username string, password string) *privateClient {
	return &privateClient{
		dialer:     dialer,
		serverAddr: serverAddr,
		method:     method,
		username:   username,
		password:   password,
	}
}

func (c *privateClient) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	network = N.NetworkName(network)
	var command byte
	switch network {
	case N.NetworkTCP:
		command = socks5.CommandConnect
	case N.NetworkUDP:
		command = socks5.CommandUDPAssociate
	default:
		return nil, E.Extend(N.ErrUnknownNetwork, network)
	}

	rawTCPConn, err := c.dialer.DialContext(ctx, N.NetworkTCP, c.serverAddr)
	if err != nil {
		return nil, err
	}
	tcpConn := newXORWriteConn(rawTCPConn)
	response, err := privateClientHandshake5(tcpConn, command, destination, c.method, c.username, c.password)
	if err != nil {
		tcpConn.Close()
		return nil, err
	}
	if command == socks5.CommandConnect {
		return tcpConn, nil
	}

	rawUDPConn, err := c.dialer.DialContext(ctx, N.NetworkUDP, response.Bind)
	if err != nil {
		tcpConn.Close()
		return nil, err
	}
	udpConn := newXORWriteConn(rawUDPConn)
	return S.NewAssociatePacketConn(udpConn, destination, tcpConn), nil
}

func (c *privateClient) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	conn, err := c.DialContext(ctx, N.NetworkUDP, destination)
	if err != nil {
		return nil, err
	}
	packetConn, loaded := conn.(net.PacketConn)
	if !loaded {
		conn.Close()
		return nil, E.New("private socks5: invalid UDP association")
	}
	return packetConn, nil
}

func privateClientHandshake5(conn io.ReadWriter, command byte, destination M.Socksaddr, method privateAuthMethod, username string, password string) (socks5.Response, error) {
	reader := varbin.StubReader(conn)
	err := socks5.WriteAuthRequest(conn, socks5.AuthRequest{Methods: []byte{byte(method)}})
	if err != nil {
		return socks5.Response{}, E.Cause(err, "private socks5: write authentication method")
	}

	challenge, err := readPrivateAuthChallenge(reader, method)
	if err != nil {
		return socks5.Response{}, err
	}
	err = writePrivateAuthRequest(conn, method, username, password, challenge)
	if err != nil {
		return socks5.Response{}, E.Cause(err, "private socks5: write authentication request")
	}
	authResponse, err := socks5.ReadUsernamePasswordAuthResponse(reader)
	if err != nil {
		return socks5.Response{}, E.Cause(err, "private socks5: read authentication response")
	}
	if authResponse.Status != socks5.UsernamePasswordStatusSuccess {
		return socks5.Response{}, E.New("private socks5: authentication failed")
	}

	if command == socks5.CommandUDPAssociate {
		if destination.Addr.IsPrivate() {
			if destination.Addr.Is6() {
				destination.Addr = netip.AddrFrom4([4]byte{127, 0, 0, 1})
			} else {
				destination.Addr = netip.IPv6Loopback()
			}
		} else if destination.Addr.IsGlobalUnicast() {
			if destination.Addr.Is6() {
				destination.Addr = netip.IPv6Unspecified()
			} else {
				destination.Addr = netip.IPv4Unspecified()
			}
		} else {
			destination.Addr = netip.IPv6Unspecified()
		}
		destination.Port = 0
	}

	err = socks5.WriteRequest(conn, socks5.Request{
		Command:     command,
		Destination: destination,
	})
	if err != nil {
		return socks5.Response{}, E.Cause(err, "private socks5: write connection request")
	}
	response, err := socks5.ReadResponse(reader)
	if err != nil {
		return socks5.Response{}, E.Cause(err, "private socks5: read connection response")
	}
	if response.ReplyCode != socks5.ReplyCodeSuccess {
		return socks5.Response{}, E.New("private socks5: connection rejected, code=", response.ReplyCode)
	}
	return response, nil
}

func readPrivateAuthChallenge(reader varbin.Reader, method privateAuthMethod) ([]byte, error) {
	version, err := reader.ReadByte()
	if err != nil {
		return nil, E.Cause(err, "private socks5: read challenge version")
	}
	if version != socks5.Version {
		return nil, E.New("private socks5: unexpected challenge version: ", version)
	}
	switch method {
	case privateAuthMethod80:
		challenge, err := reader.ReadByte()
		if err != nil {
			return nil, E.Cause(err, "private socks5: read 0x80 challenge")
		}
		return []byte{challenge}, nil
	case privateAuthMethod82:
		responseMethod, err := reader.ReadByte()
		if err != nil {
			return nil, E.Cause(err, "private socks5: read 0x82 method")
		}
		if responseMethod != byte(privateAuthMethod82) {
			return nil, E.New("private socks5: unexpected authentication method: ", responseMethod)
		}
		challenge := make([]byte, 4)
		_, err = io.ReadFull(reader, challenge)
		if err != nil {
			return nil, E.Cause(err, "private socks5: read 0x82 challenge")
		}
		return challenge, nil
	default:
		return nil, E.New("private socks5: unsupported authentication method: ", byte(method))
	}
}

func writePrivateAuthRequest(writer io.Writer, method privateAuthMethod, username string, password string, challenge []byte) error {
	var key []byte
	switch method {
	case privateAuthMethod80:
		key = make([]byte, 0, len(username)+len(password))
		key = append(key, username...)
		key = append(key, password...)
	case privateAuthMethod82:
		// MD5 is required by the peer protocol as part of the HMAC key derivation.
		passwordDigest := md5.Sum([]byte(password))
		passwordHex := make([]byte, hex.EncodedLen(len(passwordDigest)))
		hex.Encode(passwordHex, passwordDigest[:])
		key = make([]byte, 0, len(username)+len(passwordHex))
		key = append(key, username...)
		key = append(key, passwordHex...)
	default:
		return E.New("private socks5: unsupported authentication method: ", byte(method))
	}
	defer clear(key)

	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(challenge)
	signature := mac.Sum(nil)
	requestLength := 1 + 1 + len(username) + 1 + len(signature)
	if method == privateAuthMethod82 {
		requestLength += len(privateAuth82FixedData)
	}
	request := buf.NewSize(requestLength)
	defer request.Release()
	common.Must(
		request.WriteByte(1),
		request.WriteByte(byte(len(username))),
		common.Error(request.WriteString(username)),
		request.WriteByte(byte(len(signature))),
		common.Error(request.Write(signature)),
	)
	if method == privateAuthMethod82 {
		common.Must(common.Error(request.Write(privateAuth82FixedData[:])))
	}
	return common.Error(writer.Write(request.Bytes()))
}

type xorWriteConn struct {
	N.ExtendedConn
}

func newXORWriteConn(conn net.Conn) *xorWriteConn {
	return &xorWriteConn{ExtendedConn: bufio.NewExtendedConn(conn)}
}

func (c *xorWriteConn) Write(payload []byte) (int, error) {
	if len(payload) == 0 {
		return c.ExtendedConn.Write(payload)
	}
	buffer := buf.NewSize(len(payload))
	defer buffer.Release()
	common.Must(common.Error(buffer.Write(payload)))
	xorPrivatePayload(buffer.Bytes())
	return c.ExtendedConn.Write(buffer.Bytes())
}

func (c *xorWriteConn) WriteBuffer(buffer *buf.Buffer) error {
	xorPrivatePayload(buffer.Bytes())
	return c.ExtendedConn.WriteBuffer(buffer)
}

func (c *xorWriteConn) UpstreamReader() any {
	return c.ExtendedConn
}

func (c *xorWriteConn) ReaderReplaceable() bool {
	return true
}

func (c *xorWriteConn) WriterReplaceable() bool {
	return false
}

func (c *xorWriteConn) CloseRead() error {
	return N.CloseRead(c.ExtendedConn)
}

func (c *xorWriteConn) CloseWrite() error {
	return N.CloseWrite(c.ExtendedConn)
}

func xorPrivatePayload(payload []byte) {
	for index := range payload {
		payload[index] ^= 0xFF
	}
}
