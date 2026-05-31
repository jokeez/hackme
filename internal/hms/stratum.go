package hms

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
)

// RunStratumBridge serves a minimal Stratum-like TCP line protocol for SHA256 seal ASICs (dev/pilot).
func RunStratumBridge(coord *Coordinator, addr string) {
	if strings.TrimSpace(addr) == "" {
		addr = ":3334"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("[hms-stratum] listen %s: %v", addr, err)
		return
	}
	log.Printf("[hms-stratum] listening on %s (HMS_STRATUM_INSECURE=%s)", addr, os.Getenv("HMS_STRATUM_INSECURE"))
	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		go handleStratumConn(coord, conn)
	}
}

func handleStratumConn(coord *Coordinator, conn net.Conn) {
	defer conn.Close()
	sc := bufio.NewScanner(conn)
	var workerID string
	var mu sync.Mutex
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var msg struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		switch msg.Method {
		case "mining.subscribe", "mining.authorize":
			if len(msg.Params) > 0 {
				workerID, _ = msg.Params[0].(string)
			}
			replyStratum(conn, msg.ID, []any{true, "hms-seal"})
		case "mining.submit":
			nonce, err := parseStratumSubmitNonce(msg.Params)
			if err != nil {
				replyStratumErr(conn, msg.ID, "bad nonce")
				continue
			}
			work, err := coord.SealWork()
			if err != nil {
				replyStratumErr(conn, msg.ID, err.Error())
				continue
			}
			ep := int64(0)
			switch v := work["epoch_id"].(type) {
			case int64:
				ep = v
			case float64:
				ep = int64(v)
			}
			p := SealSubmitPayload{WorkerID: workerID, EpochID: ep, Nonce: nonce}
			mu.Lock()
			err = coord.SubmitSeal(p, "", "")
			mu.Unlock()
			if err != nil {
				replyStratumErr(conn, msg.ID, err.Error())
			} else {
				replyStratum(conn, msg.ID, true)
			}
		default:
			work, err := coord.SealWork()
			if err != nil {
				replyStratumErr(conn, msg.ID, err.Error())
				continue
			}
			notify := map[string]any{
				"id":     nil,
				"method": "mining.notify",
				"params": []any{work["manifest_root"], work["target"], work["epoch_id"]},
			}
			b, _ := json.Marshal(notify)
			_, _ = conn.Write(append(b, '\n'))
			if msg.ID != nil {
				replyStratum(conn, msg.ID, work)
			}
		}
	}
}

func replyStratum(conn net.Conn, id any, result any) {
	b, _ := json.Marshal(map[string]any{"id": id, "result": result, "error": nil})
	_, _ = conn.Write(append(b, '\n'))
}

func replyStratumErr(conn net.Conn, id any, msg string) {
	b, _ := json.Marshal(map[string]any{"id": id, "error": []any{21, msg, nil}})
	_, _ = conn.Write(append(b, '\n'))
}

// parseStratumSubmitNonce accepts simple [worker, nonce] or Antminer [worker, job, ex2, ntime, nonce].
func parseStratumSubmitNonce(params []any) (uint64, error) {
	if len(params) < 2 {
		return 0, errors.New("bad params")
	}
	nonceStr := fmt.Sprint(params[1])
	if len(params) >= 5 {
		nonceStr = fmt.Sprint(params[4])
	}
	nonceStr = strings.TrimSpace(nonceStr)
	if strings.HasPrefix(strings.ToLower(nonceStr), "0x") {
		return strconv.ParseUint(nonceStr[2:], 16, 64)
	}
	return strconv.ParseUint(nonceStr, 10, 64)
}
