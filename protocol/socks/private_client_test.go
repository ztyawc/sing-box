package socks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	S "github.com/sagernet/sing/protocol/socks"
	"github.com/sagernet/sing/protocol/socks/socks5"

	"github.com/stretchr/testify/require"
)

const (
	testPrivateUsername = "1234567890123456789"
	testPrivatePassword = "private-password"
)

func TestPrivateClientTCP(t *testing.T) {
	for _, method := range []privateAuthMethod{privateAuthMethod80, privateAuthMethod82} {
		t.Run(fmt.Sprintf("0x%02X", byte(method)), func(t *testing.T) {
			clientConn, serverConn := net.Pipe()
			setPipeDeadline(t, clientConn, serverConn)
			dialer := &queuedDialer{tcpConnections: []net.Conn{clientConn}}
			destination := M.ParseSocksaddrHostPort("1.2.3.4", 443)
			clientPayload := []byte("private socks tcp request")
			serverPayload := []byte("private socks tcp response")
			serverError := make(chan error, 1)
			go func() {
				defer serverConn.Close()
				serverError <- servePrivateSOCKS(serverConn, privateServerOptions{
					method:              method,
					username:            testPrivateUsername,
					password:            testPrivatePassword,
					command:             socks5.CommandConnect,
					requestDestination:  destination,
					applicationRequest:  clientPayload,
					applicationResponse: serverPayload,
				})
			}()

			client := newPrivateClient(dialer, M.ParseSocksaddrHostPort("127.0.0.1", 1080), method, testPrivateUsername, testPrivatePassword)
			conn, err := client.DialContext(context.Background(), N.NetworkTCP, destination)
			require.NoError(t, err)
			originalPayload := bytes.Clone(clientPayload)
			n, err := conn.Write(clientPayload)
			require.NoError(t, err)
			require.Equal(t, len(clientPayload), n)
			require.Equal(t, originalPayload, clientPayload, "Write must not modify the caller buffer")
			response := make([]byte, len(serverPayload))
			_, err = io.ReadFull(conn, response)
			require.NoError(t, err)
			require.Equal(t, serverPayload, response, "server data must not be XOR decoded")
			require.NoError(t, conn.Close())
			require.NoError(t, <-serverError)
		})
	}
}

func TestPrivateClientUDP(t *testing.T) {
	for _, method := range []privateAuthMethod{privateAuthMethod80, privateAuthMethod82} {
		t.Run(fmt.Sprintf("0x%02X", byte(method)), func(t *testing.T) {
			clientTCPConn, serverTCPConn := net.Pipe()
			clientUDPConn, serverUDPConn := net.Pipe()
			setPipeDeadline(t, clientTCPConn, serverTCPConn, clientUDPConn, serverUDPConn)
			dialer := &queuedDialer{
				tcpConnections: []net.Conn{clientTCPConn},
				udpConnections: []net.Conn{clientUDPConn},
			}
			destination := M.ParseSocksaddrHostPort("8.8.8.8", 53)
			bindDestination := M.ParseSocksaddrHostPort("127.0.0.1", 9000)
			serverError := make(chan error, 1)
			udpFinished := make(chan struct{})
			go func() {
				defer serverTCPConn.Close()
				err := servePrivateSOCKS(serverTCPConn, privateServerOptions{
					method:             method,
					username:           testPrivateUsername,
					password:           testPrivatePassword,
					command:            socks5.CommandUDPAssociate,
					requestDestination: M.SocksaddrFrom(netip.IPv4Unspecified(), 0),
					bindDestination:    bindDestination,
				})
				serverError <- err
				if err == nil {
					<-udpFinished
				}
			}()

			udpRequest := []byte("private socks udp request")
			udpResponse := []byte("private socks udp response")
			udpServerError := make(chan error, 1)
			go func() {
				defer serverUDPConn.Close()
				expectedRequest, err := buildSOCKS5Datagram(destination, udpRequest)
				if err != nil {
					udpServerError <- err
					return
				}
				actualRequest, err := readXORPayload(serverUDPConn, len(expectedRequest))
				if err != nil {
					udpServerError <- err
					return
				}
				if !bytes.Equal(expectedRequest, actualRequest) {
					udpServerError <- fmt.Errorf("unexpected UDP request: %x", actualRequest)
					return
				}
				response, err := buildSOCKS5Datagram(destination, udpResponse)
				if err == nil {
					_, err = serverUDPConn.Write(response)
				}
				udpServerError <- err
			}()

			client := newPrivateClient(dialer, M.ParseSocksaddrHostPort("127.0.0.1", 1080), method, testPrivateUsername, testPrivatePassword)
			packetConn, err := client.ListenPacket(context.Background(), destination)
			require.NoError(t, err)
			requestCopy := bytes.Clone(udpRequest)
			n, err := packetConn.WriteTo(udpRequest, destination.UDPAddr())
			require.NoError(t, err)
			require.Equal(t, 3+M.SocksaddrSerializer.AddrPortLen(destination)+len(udpRequest), n)
			require.Equal(t, requestCopy, udpRequest)
			response := make([]byte, 1024)
			n, responseAddr, err := packetConn.ReadFrom(response)
			require.NoError(t, err)
			require.Equal(t, udpResponse, response[:n])
			require.Equal(t, destination.String(), responseAddr.String())
			require.NoError(t, packetConn.Close())
			close(udpFinished)
			require.NoError(t, <-serverError)
			require.NoError(t, <-udpServerError)
		})
	}
}

func TestPrivateAuthenticationErrors(t *testing.T) {
	tests := []struct {
		name     string
		method   privateAuthMethod
		response []byte
	}{
		{"invalid challenge version", privateAuthMethod80, []byte{0x04, 0x01}},
		{"invalid 0x82 method", privateAuthMethod82, []byte{0x05, 0x80}},
		{"short 0x82 challenge", privateAuthMethod82, []byte{0x05, 0x82, 0x01, 0x02}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clientConn, serverConn := net.Pipe()
			setPipeDeadline(t, clientConn, serverConn)
			go func() {
				defer serverConn.Close()
				_, _ = readXORPayload(serverConn, 3)
				_, _ = serverConn.Write(test.response)
			}()
			_, err := privateClientHandshake5(newXORWriteConn(clientConn), socks5.CommandConnect, M.ParseSocksaddrHostPort("1.2.3.4", 443), test.method, testPrivateUsername, testPrivatePassword)
			require.Error(t, err)
		})
	}
}

func TestPrivateAuthenticationFailure(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	setPipeDeadline(t, clientConn, serverConn)
	go func() {
		defer serverConn.Close()
		_, _ = readXORPayload(serverConn, 3)
		_, _ = serverConn.Write([]byte{0x05, 0x5A})
		_, _ = readXORPayload(serverConn, 54)
		_, _ = serverConn.Write([]byte{0x01, 0x01})
	}()
	_, err := privateClientHandshake5(newXORWriteConn(clientConn), socks5.CommandConnect, M.ParseSocksaddrHostPort("1.2.3.4", 443), privateAuthMethod80, testPrivateUsername, testPrivatePassword)
	require.ErrorContains(t, err, "authentication failed")
	require.NotContains(t, err.Error(), testPrivatePassword)
}

func TestPrivateAuthOptionValidation(t *testing.T) {
	method, err := parsePrivateAuthMethod("")
	require.NoError(t, err)
	require.Zero(t, method)
	require.NoError(t, validatePrivateAuthOptions(S.Version4, method, "", ""), "standard SOCKS settings must remain unchanged")

	method, err = parsePrivateAuthMethod("0x80")
	require.NoError(t, err)
	require.Equal(t, privateAuthMethod80, method)
	require.NoError(t, validatePrivateAuthOptions(S.Version5, method, testPrivateUsername, testPrivatePassword))

	method, err = parsePrivateAuthMethod("0x82")
	require.NoError(t, err)
	require.Equal(t, privateAuthMethod82, method)
	require.NoError(t, validatePrivateAuthOptions(S.Version5, method, testPrivateUsername, testPrivatePassword))

	_, err = parsePrivateAuthMethod("80")
	require.Error(t, err)
	require.Error(t, validatePrivateAuthOptions(S.Version4, privateAuthMethod80, testPrivateUsername, testPrivatePassword))
	require.Error(t, validatePrivateAuthOptions(S.Version5, privateAuthMethod80, "short", testPrivatePassword))
	require.Error(t, validatePrivateAuthOptions(S.Version5, privateAuthMethod80, testPrivateUsername, ""))
}

func TestXORWriteConn(t *testing.T) {
	payload := bytes.Repeat([]byte{0x00, 0x55, 0xAA, 0xFF}, 256*1024)
	original := bytes.Clone(payload)
	underlying := &recordingConn{}
	conn := newXORWriteConn(underlying)
	n, err := conn.Write(payload)
	require.NoError(t, err)
	require.Equal(t, len(payload), n)
	require.Equal(t, original, payload)
	expected := bytes.Clone(payload)
	xorPrivatePayload(expected)
	require.Equal(t, expected, underlying.Bytes())
}

func TestXORWriteConnPartialWrite(t *testing.T) {
	payload := []byte{0x10, 0x20, 0x30, 0x40}
	original := bytes.Clone(payload)
	underlying := &recordingConn{writeLimit: 2, writeError: io.ErrShortWrite}
	conn := newXORWriteConn(underlying)
	n, err := conn.Write(payload)
	require.ErrorIs(t, err, io.ErrShortWrite)
	require.Equal(t, 2, n)
	require.Equal(t, original, payload)
	require.Equal(t, []byte{0xEF, 0xDF}, underlying.Bytes())
}

func TestXORWriteBuffer(t *testing.T) {
	payload := []byte("buffered payload")
	underlying := &recordingConn{}
	conn := newXORWriteConn(underlying)
	buffer := buf.NewSize(len(payload))
	_, err := buffer.Write(payload)
	require.NoError(t, err)
	require.NoError(t, conn.WriteBuffer(buffer))
	expected := bytes.Clone(payload)
	xorPrivatePayload(expected)
	require.Equal(t, expected, underlying.Bytes())
}

type privateServerOptions struct {
	method              privateAuthMethod
	username            string
	password            string
	command             byte
	requestDestination  M.Socksaddr
	bindDestination     M.Socksaddr
	applicationRequest  []byte
	applicationResponse []byte
}

func servePrivateSOCKS(conn net.Conn, options privateServerOptions) error {
	methodRequest, err := readXORPayload(conn, 3)
	if err != nil {
		return err
	}
	expectedMethodRequest := []byte{socks5.Version, 0x01, byte(options.method)}
	if !bytes.Equal(expectedMethodRequest, methodRequest) {
		return fmt.Errorf("unexpected method request: %x", methodRequest)
	}
	challenge := privateTestChallenge(options.method)
	if options.method == privateAuthMethod80 {
		_, err = conn.Write([]byte{socks5.Version, challenge[0]})
	} else {
		_, err = conn.Write(append([]byte{socks5.Version, byte(privateAuthMethod82)}, challenge...))
	}
	if err != nil {
		return err
	}
	authRequestLength := 54
	if options.method == privateAuthMethod82 {
		authRequestLength = 75
	}
	authRequest, err := readXORPayload(conn, authRequestLength)
	if err != nil {
		return err
	}
	expectedAuthRequest := buildExpectedPrivateAuthRequest(options.method, options.username, options.password, challenge)
	if !bytes.Equal(expectedAuthRequest, authRequest) {
		return fmt.Errorf("unexpected authentication request: %x", authRequest)
	}
	_, err = conn.Write([]byte{0x01, 0x00})
	if err != nil {
		return err
	}

	var expectedRequest bytes.Buffer
	err = socks5.WriteRequest(&expectedRequest, socks5.Request{Command: options.command, Destination: options.requestDestination})
	if err != nil {
		return err
	}
	request, err := readXORPayload(conn, expectedRequest.Len())
	if err != nil {
		return err
	}
	if !bytes.Equal(expectedRequest.Bytes(), request) {
		return fmt.Errorf("unexpected connection request: %x", request)
	}
	bindDestination := options.bindDestination
	if !bindDestination.IsValid() {
		bindDestination = M.ParseSocksaddrHostPort("127.0.0.1", 1080)
	}
	err = socks5.WriteResponse(conn, socks5.Response{ReplyCode: socks5.ReplyCodeSuccess, Bind: bindDestination})
	if err != nil {
		return err
	}
	if options.applicationRequest == nil {
		return nil
	}
	request, err = readXORPayload(conn, len(options.applicationRequest))
	if err != nil {
		return err
	}
	if !bytes.Equal(options.applicationRequest, request) {
		return fmt.Errorf("unexpected application request: %x", request)
	}
	_, err = conn.Write(options.applicationResponse)
	return err
}

func privateTestChallenge(method privateAuthMethod) []byte {
	if method == privateAuthMethod80 {
		return []byte{0x5A}
	}
	return []byte{0x01, 0x23, 0x45, 0x67}
}

func buildExpectedPrivateAuthRequest(method privateAuthMethod, username string, password string, challenge []byte) []byte {
	key := []byte(username + password)
	if method == privateAuthMethod82 {
		passwordDigest := md5.Sum([]byte(password))
		key = []byte(username + hex.EncodeToString(passwordDigest[:]))
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(challenge)
	request := []byte{0x01, byte(len(username))}
	request = append(request, username...)
	request = append(request, byte(sha256.Size))
	request = append(request, mac.Sum(nil)...)
	if method == privateAuthMethod82 {
		request = append(request, privateAuth82FixedData[:]...)
	}
	return request
}

func buildSOCKS5Datagram(destination M.Socksaddr, payload []byte) ([]byte, error) {
	var packet bytes.Buffer
	packet.Write([]byte{0x00, 0x00, 0x00})
	err := M.SocksaddrSerializer.WriteAddrPort(&packet, destination)
	if err != nil {
		return nil, err
	}
	packet.Write(payload)
	return packet.Bytes(), nil
}

func readXORPayload(reader io.Reader, size int) ([]byte, error) {
	payload := make([]byte, size)
	_, err := io.ReadFull(reader, payload)
	if err != nil {
		return nil, err
	}
	xorPrivatePayload(payload)
	return payload, nil
}

func setPipeDeadline(t *testing.T, connections ...net.Conn) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for _, conn := range connections {
		require.NoError(t, conn.SetDeadline(deadline))
	}
}

type queuedDialer struct {
	access         sync.Mutex
	tcpConnections []net.Conn
	udpConnections []net.Conn
}

func (d *queuedDialer) DialContext(_ context.Context, network string, _ M.Socksaddr) (net.Conn, error) {
	d.access.Lock()
	defer d.access.Unlock()
	var connections *[]net.Conn
	if N.NetworkName(network) == N.NetworkTCP {
		connections = &d.tcpConnections
	} else {
		connections = &d.udpConnections
	}
	if len(*connections) == 0 {
		return nil, errors.New("no queued connection")
	}
	conn := (*connections)[0]
	*connections = (*connections)[1:]
	return conn, nil
}

func (d *queuedDialer) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, errors.New("not implemented")
}

type recordingConn struct {
	bytes.Buffer
	writeLimit int
	writeError error
}

func (c *recordingConn) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (c *recordingConn) Write(payload []byte) (int, error) {
	writeLength := len(payload)
	if c.writeLimit > 0 && c.writeLimit < writeLength {
		writeLength = c.writeLimit
	}
	_, _ = c.Buffer.Write(payload[:writeLength])
	return writeLength, c.writeError
}

func (c *recordingConn) Close() error                     { return nil }
func (c *recordingConn) LocalAddr() net.Addr              { return testAddr("local") }
func (c *recordingConn) RemoteAddr() net.Addr             { return testAddr("remote") }
func (c *recordingConn) SetDeadline(time.Time) error      { return nil }
func (c *recordingConn) SetReadDeadline(time.Time) error  { return nil }
func (c *recordingConn) SetWriteDeadline(time.Time) error { return nil }

type testAddr string

func (a testAddr) Network() string { return "test" }
func (a testAddr) String() string  { return string(a) }
