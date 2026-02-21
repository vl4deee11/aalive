package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sync"

	"github.com/gorilla/websocket"

	"github.com/vl4deee11/aalive/sim"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

type Client struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (c *Client) Send(v interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteJSON(v)
}

type ClientManager struct {
	clients map[*Client]struct{}
	mu      sync.RWMutex
}

func NewClientManager() *ClientManager {
	return &ClientManager{
		clients: make(map[*Client]struct{}),
	}
}

func (cm *ClientManager) AddClient(c *Client) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.clients[c] = struct{}{}
}

func (cm *ClientManager) RemoveClient(c *Client) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.clients, c)
}

func (cm *ClientManager) GetClients() []*Client {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	list := make([]*Client, 0, len(cm.clients))
	for c := range cm.clients {
		list = append(list, c)
	}
	return list
}

func main() {
	width, height := 100, 100
	s := sim.NewSim(width, height)
	go s.Run()

	clientManager := NewClientManager()

	go func() {
		for state := range s.StateChan {
			for _, c := range clientManager.GetClients() {
				if err := c.Send(state); err != nil {
					log.Printf("client send error: %v", err)
					clientManager.RemoveClient(c)
					c.conn.Close()
				}
			}
		}
	}()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("upgrade:", err)
			return
		}
		client := &Client{conn: conn}
		clientManager.AddClient(client)

		if err := client.Send(map[string]interface{}{"type": "config", "w": width, "h": height}); err != nil {
			log.Printf("send config error: %v", err)
			clientManager.RemoveClient(client)
			conn.Close()
			return
		}

		for {
			var msg map[string]interface{}
			if err := conn.ReadJSON(&msg); err != nil {
				log.Printf("read json error: %v", err)
				break
			}
			typeStr, _ := msg["type"].(string)
			switch typeStr {
			case "add_food":
				if mx, ok := msg["x"].(float64); ok {
					if my, ok2 := msg["y"].(float64); ok2 {
						energy := 12.0
						if e, ok3 := msg["energy"].(float64); ok3 {
							energy = e
						}
						s.AddFoodAt(int(mx), int(my), energy)
					}
				}
			case "toggle_random_food":
				if en, ok := msg["enabled"].(bool); ok {
					s.SetRandomFood(en)
				}
			case "add_agent":
				if mx, ok := msg["x"].(float64); ok {
					if my, ok2 := msg["y"].(float64); ok2 {
						energy := 40.0
						if e, ok3 := msg["energy"].(float64); ok3 {
							energy = e
						}
						sexStr := "M"
						if ss, ok4 := msg["sex"].(string); ok4 {
							sexStr = ss
						}
						agg := 0.5
						if a2, ok5 := msg["agg"].(float64); ok5 {
							agg = a2
						}
						spd := 1
						if sp, ok6 := msg["spd"].(float64); ok6 {
							spd = int(sp)
						}
						str := 5.0
						if st, ok7 := msg["strength"].(float64); ok7 {
							str = st
						}
						repro := 0.05
						if rp, ok8 := msg["repro"].(float64); ok8 {
							repro = rp
						}
						sex := "M"
						if sexStr == "F" {
							sex = "F"
						}
						s.AddAgentAt(int(mx), int(my), energy, sim.Sex(sex), agg, spd, str, repro)
					}
				}
			default:
			}
			if err := client.Send(map[string]string{"ok": "received"}); err != nil {
				log.Printf("send ok error: %v", err)
				break
			}
		}

		clientManager.RemoveClient(client)
		conn.Close()
	})

	http.Handle("/", http.FileServer(http.Dir("static")))

	basePort := 8080
	if p := os.Getenv("PORT"); p != "" {
		fmt.Sscanf(p, "%d", &basePort)
	}

	started := false
	for i := 0; i < 10; i++ {
		port := basePort + i
		addr := fmt.Sprintf(":%d", port)
		log.Printf("Trying to start server on %s", addr)
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			log.Printf("failed to listen on %s: %v", addr, err)
			continue
		}
		log.Printf("Server started at http://localhost:%d", port)
		started = true
		if err := http.Serve(ln, nil); err != nil {
			log.Fatalf("http serve error: %v", err)
		}
		break
	}
	if !started {
		log.Fatal("unable to start server on any port")
	}
}
