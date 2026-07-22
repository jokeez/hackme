package hms

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
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

// RunStratumBridge serves a minimal Stratum-like TCP line protocol for SHA256 seal ASICs.
// Default bind is loopback-only. Non-loopback requires HMS_STRATUM_HMAC_SECRET, or both
// HMS_STRATUM_INSECURE=1 and HMS_STRATUM_ALLOW_PUBLIC=1 (lab only) — H47 / HMC-002.
func RunStratumBridge(coord *Coordinator, addr string) {
	if strings.TrimSpace(addr) == "" {
		addr = "127.0.0.1:3334"
	}
	if err := stratumBindAllowed(addr); err != nil {
		log.Printf("[hms-stratum] refusing listen %s: %v", addr, err)
		return
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("[hms-stratum] listen %s: %v", addr, err)
		return
	}
	log.Printf("[hms-stratum] listening on %s (insecure=%v hmac=%v)", addr, stratumInsecureEnabled(), stratumHMACSecret() != "")
	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		go handleStratumConn(coord, conn)
	}
}

func stratumHMACSecret() string {
	for _, k := range []string{
		"HMS_STRATUM_HMAC_SECRET",
		"HACKME_HMS_STRATUM_HMAC_SECRET",
		"HMS_STRATUM_WORKER_HMAC_SECRET",
	} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func stratumAllowPublic() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HMS_STRATUM_ALLOW_PUBLIC")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func stratumBindHost(addr string) string {
	host := strings.TrimSpace(addr)
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = strings.TrimSpace(h)
	} else if strings.HasPrefix(addr, ":") {
		return ""
	}
	return strings.Trim(strings.ToLower(host), "[]")
}

func stratumBindIsLoopback(addr string) bool {
	host := stratumBindHost(addr)
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func stratumBindAllowed(addr string) error {
	if stratumBindIsLoopback(addr) {
		return nil
	}
	if stratumHMACSecret() != "" {
		return nil
	}
	if stratumInsecureEnabled() && stratumAllowPublic() {
		return nil
	}
	return errors.New("non-loopback Stratum requires HMS_STRATUM_HMAC_SECRET, or HMS_STRATUM_INSECURE=1 + HMS_STRATUM_ALLOW_PUBLIC=1")
}

func stratumPasswordOK(workerID, password, secret string) bool {
	workerID = strings.TrimSpace(workerID)
	password = strings.TrimSpace(strings.ToLower(password))
	if workerID == "" || password == "" || secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(workerID))
	want := hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(password), []byte(want)) == 1
}

func handleStratumConn(coord *Coordinator, conn net.Conn) {
	defer conn.Close()
	sc := bufio.NewScanner(conn)
	var workerID string
	var authorized bool
	var hmacOK bool
	var mu sync.Mutex
	secret := stratumHMACSecret()
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
		case "mining.subscribe":
			if len(msg.Params) > 0 {
				workerID, _ = msg.Params[0].(string)
			}
			replyStratum(conn, msg.ID, []any{true, "hms-seal"})
		case "mining.authorize":
			if len(msg.Params) > 0 {
				workerID, _ = msg.Params[0].(string)
			}
			password := ""
			if len(msg.Params) > 1 {
				password = fmt.Sprint(msg.Params[1])
			}
			workerID = strings.TrimSpace(workerID)
			if secret != "" {
				if !stratumPasswordOK(workerID, password, secret) {
					authorized = false
					hmacOK = false
					replyStratumErr(conn, msg.ID, "unauthorized")
					continue
				}
				authorized = true
				hmacOK = true
			} else if stratumInsecureEnabled() {
				authorized = true
				hmacOK = false
			} else {
				authorized = false
				replyStratumErr(conn, msg.ID, "set HMS_STRATUM_HMAC_SECRET or HMS_STRATUM_INSECURE=1 on loopback")
				continue
			}
			replyStratum(conn, msg.ID, []any{true, "hms-seal"})
		case "mining.submit":
			if !authorized {
				replyStratumErr(conn, msg.ID, "unauthorized")
				continue
			}
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
			wid := workerID
			if len(msg.Params) > 0 {
				if s, ok := msg.Params[0].(string); ok && strings.TrimSpace(s) != "" {
					wid = strings.TrimSpace(s)
				}
			}
			p := SealSubmitPayload{WorkerID: wid, EpochID: ep, Nonce: nonce}
			mu.Lock()
			err = coord.SubmitSealFromStratum(p, hmacOK)
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
