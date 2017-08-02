package senso

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

// Handle for managing Senso
type Handle struct {
	Data    chan []byte
	Control chan []byte

	ctx context.Context

	log *logrus.Entry
}

// New returns an initialized Senso handler
func New(ctx context.Context, log *logrus.Entry) *Handle {
	handle := Handle{}

	handle.ctx = ctx

	handle.log = log

	// Make channels
	handle.Data = make(chan []byte)
	handle.Control = make(chan []byte)

	return &handle
}

// Connect to a Senso, will create TCP connections to control and data ports
func (handle *Handle) Connect(address string) context.CancelFunc {
	// Create a child context for a new connection. This allows an individual connection (attempt) to be cancelled without restarting the whole Senso handler
	ctx, cancel := context.WithCancel(handle.ctx)

	handle.log.WithField("address", address).Info("attempting to connect")

	go connectTCP(ctx, handle.log.WithField("channel", "data"), address+":55568", handle.Data)
	go connectTCP(ctx, handle.log.WithField("channel", "control"), address+":55567", handle.Control)

	return cancel
}

// connectTCP creates a persistent tcp connection to address
func connectTCP(ctx context.Context, baseLogger *logrus.Entry, address string, data chan []byte) {
	var dialer net.Dialer

	var log = baseLogger.WithField("address", address)

	// loop to retry connection
	for {
		// attempt to open a new connection
		dialer.Deadline = time.Now().Add(1 * time.Second)
		log.Info("dialing")
		conn, connErr := dialer.DialContext(ctx, "tcp", address)

		if connErr != nil {
			log.Println(connErr.Error())
			log.WithError(connErr).Info("dial failed")
		} else {

			log.Info("connected")

			// Close connection if we break or return
			defer conn.Close()

			// create channel for reading data and go read
			readChannel := make(chan []byte)
			go tcpReader(log, conn, readChannel)

			// create channel for writing data
			writeChannel := make(chan []byte)
			// We need an additional channel for handling write errors, unlike the readChannel we don't want to close the channel as somebody might try to write to it
			writeErrors := make(chan error)
			defer close(writeChannel)
			go tcpWriter(conn, writeChannel, writeErrors)

			// Inner loop for handling data
		DataLoop:
			for {
				select {

				case <-ctx.Done():
					return

				case receivedData, more := <-readChannel:
					if more {
						// Attempt to send data, if can not send immediately discard
						select {
						case data <- receivedData:
						default:
						}
					} else {
						close(writeChannel)
						break DataLoop
					}

				case dataToWrite := <-data:
					writeChannel <- dataToWrite

				case writeError := <-writeErrors:
					if err, ok := writeError.(net.Error); ok && err.Timeout() {
						log.Debug("timeout")
					} else {
						log.WithError(writeError).Error("write error")
						close(writeChannel)
						break DataLoop
					}
				}
			}

		}
		// Check if connection has been cancelled
		select {
		case <-ctx.Done():
			return
		default:
			// Sleep 5s before reattempting to reconnect
			time.Sleep(5 * time.Second)
		}

	}
}

// Helper to read from TCP connection
func tcpReader(log *logrus.Entry, conn net.Conn, channel chan<- []byte) {

	defer close(channel)

	buffer := make([]byte, 1024)

	// Loop and read from connection.
	for {
		// conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		readN, readErr := conn.Read(buffer)

		if readErr != nil {
			if readErr == io.EOF {
				// connection is closed
				log.Info("connection closed")
				return
			} else if err, ok := readErr.(net.Error); ok && err.Timeout() {
				// Read timeout, just continue Nothing
			} else {
				log.WithError(readErr).Error("read error")
				return
			}
		} else {
			channel <- buffer[:readN]
		}
	}
}

// Helper to write to TCP connection. Note that this requires an additional channel to report errors
func tcpWriter(conn net.Conn, channel <-chan []byte, errorChannel chan<- error) {
	for {

		dataToWrite, more := <-channel

		if more {

			if conn != nil {
				conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
				_, err := conn.Write(dataToWrite)

				if err != nil {
					errorChannel <- err
				}

			} else {
				errorChannel <- errors.New("not connected, can not write to TCP connection")
			}

		} else {
			return
		}

	}
}

// Implement net/http Handler interface
var webSocketUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (handle *Handle) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	var log = handle.log.WithFields(logrus.Fields{
		"address":   r.RemoteAddr,
		"userAgent": r.UserAgent(),
	})

	// Update to WebSocket
	conn, err := webSocketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.WithError(err).Error("websocket upgrade error")
		http.Error(w, "WebSocket upgrade error", http.StatusBadRequest)
		return
	}

	log.Info("WebSocket connection opened")

	// send data
	go func() {
		for data := range handle.Data {
			// fmt.Println(data)
			conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
			defer conn.Close()
			err := conn.WriteMessage(websocket.BinaryMessage, data)
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					log.WithError(err).Error("WebSocket error")
				}
				return
			}
		}
	}()

	// receive messages
	go func() {
		defer conn.Close()
		for {
			messageType, b, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					log.WithError(err).Error("WebSocket error")
				}
				return
			}

			if messageType == websocket.BinaryMessage {
				handle.Control <- b
			} else {
				log.Debug("Got a non binary message! TODO: handle it")
			}

		}
	}()

}
