package chat

import (
	"fmt"
	"net/http"

	"github.com/gorilla/websocket"

	"microcoreos-go/core"
	"microcoreos-go/tools/httptool"
)

// ChatPlugin demonstrates real-time bidirectional communication using WebSockets.
type ChatPlugin struct {
	core.BasePluginDefaults
	http httptool.HttpTool
}

func init() {
	core.RegisterPlugin(func() core.Plugin { return &ChatPlugin{} })
}

func (p *ChatPlugin) Name() string { return "ChatPlugin" }

func (p *ChatPlugin) Inject(c *core.Container) error {
	var err error
	p.http, err = core.GetTool[httptool.HttpTool](c, "http")
	return err
}

func (p *ChatPlugin) OnBoot() error {
	p.http.AddWebsocket("/ws/chat", p.chatHandler)
	return nil
}

// chatHandler manages a single WebSocket connection.
// It reads messages and echoes them back with a prefix.
func (p *ChatPlugin) chatHandler(ws *websocket.Conn, r *http.Request) {
	fmt.Printf("[ChatPlugin] New connection from %s\n", r.RemoteAddr)

	for {
		// Read message from browser
		messageType, message, err := ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				fmt.Printf("[ChatPlugin] Error reading message: %v\n", err)
			}
			break
		}

		// Print incoming message
		fmt.Printf("[ChatPlugin] Received: %s\n", string(message))

		// Echo back with a prefix
		response := []byte("Echo: " + string(message))
		if err := ws.WriteMessage(messageType, response); err != nil {
			fmt.Printf("[ChatPlugin] Error writing message: %v\n", err)
			break
		}
	}

	fmt.Printf("[ChatPlugin] Connection closed: %s\n", r.RemoteAddr)
}
