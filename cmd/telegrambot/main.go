// Command telegrambot is a small Telegram operator bot for HackMe nodes: chain tip,
// mining telemetry, wallet snapshot, recent block journal, and optional new-height alerts.
//
// Env (required): TELEGRAM_BOT_TOKEN or HACKME_TELEGRAM_BOT_TOKEN
// Env: HACKME_TELEGRAM_NODE_URL (default http://127.0.0.1:8080)
// Env: HACKME_TELEGRAM_ALLOWED_USER_IDS — comma-separated Telegram user IDs (recommended on public hosts)
// Env: HACKME_ADMIN_TOKEN — optional X-Hackme-Admin-Token for node (GET routes used here are public)
// Env: HACKME_TELEGRAM_WATCH_POLL_SEC — watcher interval 20..600 (default 45)
//
// Example: copy scripts/ops/telegram_bot.env.example → .env.telegram, fill in token, then:
//
//	go run ./cmd/telegrambot
//
// Or: go run ./cmd/telegrambot -config /path/to/operator.env
// Shell variables always override values from the env file.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

func main() {
	configPath := flag.String("config", "", "path to env file (KEY=VALUE). If empty: $HACKME_TELEGRAM_CONFIG, else first of .env.telegram, telegram_bot.env in cwd")
	showHelp := flag.Bool("help", false, "print configuration help and exit")
	flag.Parse()
	if *showHelp {
		printCLIHelp()
		return
	}
	if err := applyTelegramEnvFiles(*configPath); err != nil {
		log.Fatalf("env file: %v", err)
	}
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("telegrambot: node=%s allowed_users=%d poll_sec=%d",
		cfg.nodeBase, len(cfg.allowedUsers), cfg.watchPollSec)

	b := newBot(cfg)
	ctx := context.Background()
	if err := b.run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}

type config struct {
	token         string
	nodeBase      string
	adminToken    string
	allowedUsers  map[int64]struct{}
	watchPollSec  int
	httpTimeout   time.Duration
	tgPollTimeout int // getUpdates long-poll
}

func loadConfig() (config, error) {
	var c config
	c.token = strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if c.token == "" {
		c.token = strings.TrimSpace(os.Getenv("HACKME_TELEGRAM_BOT_TOKEN"))
	}
	if c.token == "" {
		return c, errors.New("missing bot token: set TELEGRAM_BOT_TOKEN (or HACKME_TELEGRAM_BOT_TOKEN) in the environment, in .env.telegram, or use -config path (see: go run ./cmd/telegrambot -help)")
	}
	c.nodeBase = strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_TELEGRAM_NODE_URL")), "/")
	if c.nodeBase == "" {
		c.nodeBase = "http://127.0.0.1:8080"
	}
	c.adminToken = strings.TrimSpace(os.Getenv("HACKME_ADMIN_TOKEN"))
	if s := strings.TrimSpace(os.Getenv("HACKME_TELEGRAM_ALLOWED_USER_IDS")); s != "" {
		c.allowedUsers = make(map[int64]struct{})
		for _, p := range strings.Split(s, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			id, err := strconv.ParseInt(p, 10, 64)
			if err != nil {
				return c, fmt.Errorf("HACKME_TELEGRAM_ALLOWED_USER_IDS: invalid id %q", p)
			}
			c.allowedUsers[id] = struct{}{}
		}
	}
	c.watchPollSec = 45
	if v := strings.TrimSpace(os.Getenv("HACKME_TELEGRAM_WATCH_POLL_SEC")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.watchPollSec = n
		}
	}
	if c.watchPollSec < 20 {
		c.watchPollSec = 20
	}
	if c.watchPollSec > 600 {
		c.watchPollSec = 600
	}
	c.tgPollTimeout = 50
	// Single client: Telegram long-poll must exceed getUpdates timeout.
	c.httpTimeout = time.Duration(c.tgPollTimeout+25) * time.Second
	return c, nil
}

type bot struct {
	cfg     config
	http    *http.Client
	offset  int64
	watches sync.Map // chatID -> *watcher
}

func newBot(cfg config) *bot {
	return &bot{
		cfg: cfg,
		http: &http.Client{
			Timeout: cfg.httpTimeout,
		},
	}
}

func (b *bot) run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		updates, err := b.tgGetUpdates(ctx)
		if err != nil {
			log.Printf("getUpdates: %v", err)
			time.Sleep(3 * time.Second)
			continue
		}
		for _, u := range updates {
			if u.UpdateID >= b.offset {
				b.offset = u.UpdateID + 1
			}
			if cq := u.CallbackQuery; cq != nil {
				b.handleCallback(ctx, cq)
				continue
			}
			if u.Message == nil {
				continue
			}
			m := u.Message
			if m.From == nil {
				continue
			}
			if !b.allowed(m.From.ID) {
				_ = b.tgSendMessage(ctx, m.Chat.ID, deniedMsg(), nil)
				continue
			}
			text := strings.TrimSpace(m.Text)
			if text == "" {
				continue
			}
			b.handleCommand(ctx, m.Chat.ID, m.From.ID, text)
		}
	}
}

func deniedMsg() string {
	return escHTML("Access denied. Ask the operator to add your Telegram user id to HACKME_TELEGRAM_ALLOWED_USER_IDS.")
}

func (b *bot) allowed(userID int64) bool {
	if len(b.cfg.allowedUsers) == 0 {
		return true
	}
	_, ok := b.cfg.allowedUsers[userID]
	return ok
}

func (b *bot) handleCommand(ctx context.Context, chatID, fromID int64, text string) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return
	}
	cmd := strings.ToLower(parts[0])
	// Support /cmd@BotName
	if i := strings.IndexByte(cmd, '@'); i >= 0 {
		cmd = cmd[:i]
	}
	args := parts[1:]

	switch cmd {
	case "/start", "/help":
		b.reply(ctx, chatID, helpText(b.cfg.nodeBase), mainKeyboard())
	case "/status":
		b.reply(ctx, chatID, b.fmtStatus(ctx), statusKeyboard())
	case "/metrics":
		b.reply(ctx, chatID, b.fmtMetrics(ctx), metricsKeyboard())
	case "/wallet":
		b.reply(ctx, chatID, b.fmtWallet(ctx), walletKeyboard())
	case "/blocks":
		n := 8
		if len(args) > 0 {
			if v, err := strconv.Atoi(args[0]); err == nil && v > 0 {
				n = v
			}
		}
		if n > 25 {
			n = 25
		}
		b.reply(ctx, chatID, b.fmtBlocks(ctx, n), blocksKeyboard(n))
	case "/digest":
		b.reply(ctx, chatID, b.fmtDigest(ctx), digestKeyboard())
	case "/worker":
		b.reply(ctx, chatID, b.fmtWorker(ctx), workerKeyboard())
	case "/pool":
		b.reply(ctx, chatID, b.fmtPool(ctx), poolKeyboard())
	case "/tasks":
		b.reply(ctx, chatID, b.fmtTasks(ctx), tasksKeyboard())
	case "/watch":
		b.startWatch(ctx, chatID)
	case "/unwatch":
		b.stopWatch(chatID)
		_ = b.tgSendMessage(ctx, chatID, escHTML("Alerts stopped for this chat."), mainKeyboard())
	case "/about":
		b.reply(ctx, chatID, b.fmtAbout(ctx), mainKeyboard())
	default:
		_ = b.tgSendMessage(ctx, chatID, escHTML("Unknown command. Tap /help for the list."), mainKeyboard())
	}
}

func helpText(nodeBase string) string {
	var sb strings.Builder
	sb.WriteString("<b>HackMe operator</b>\n")
	sb.WriteString("Read-only views of your node JSON API.\n")
	sb.WriteString(fmt.Sprintf("<b>Node</b>: <code>%s</code>\n\n", escHTML(nodeBase)))
	sb.WriteString("<b>Commands</b>\n")
	sb.WriteString("/digest — chain + mining + wallet snapshot\n")
	sb.WriteString("/status — tip, genesis, network flags\n")
	sb.WriteString("/metrics — PoH throughput, target M, session solves\n")
	sb.WriteString("/wallet — balance and display mode\n")
	sb.WriteString("/worker — pool worker subprocess (coordinator mode)\n")
	sb.WriteString("/pool — global hashrate, active rigs, coordinator counters\n")
	sb.WriteString("/tasks — open fuzzing/audit orders (escrow tasks)\n")
	sb.WriteString("/blocks [n] — last blocks (default 8, max 25)\n")
	sb.WriteString("/watch — notify when <code>tip_height</code> increases\n")
	sb.WriteString("/unwatch — stop alerts\n")
	sb.WriteString("/about — build id and chain id\n")
	sb.WriteString("/help — this message\n")
	return sb.String()
}

func mainKeyboard() [][]inlineBtn {
	return [][]inlineBtn{
		{{Text: "📊 Digest", CallbackData: "r:d"}, {Text: "⛓ Status", CallbackData: "r:s"}},
		{{Text: "🌐 Pool", CallbackData: "r:p"}, {Text: "📋 Tasks", CallbackData: "r:t"}},
		{{Text: "⚙ Metrics", CallbackData: "r:m"}, {Text: "👛 Wallet", CallbackData: "r:w"}},
		{{Text: "🖥 Worker", CallbackData: "r:k"}, {Text: "📜 Blocks", CallbackData: "r:b8"}},
		{{Text: "ℹ About", CallbackData: "r:a"}},
	}
}

func statusKeyboard() [][]inlineBtn {
	return [][]inlineBtn{{{Text: "↻ Status", CallbackData: "r:s"}}}
}

func metricsKeyboard() [][]inlineBtn {
	return [][]inlineBtn{{{Text: "↻ Metrics", CallbackData: "r:m"}}}
}

func walletKeyboard() [][]inlineBtn {
	return [][]inlineBtn{{{Text: "↻ Wallet", CallbackData: "r:w"}}}
}

func blocksKeyboard(n int) [][]inlineBtn {
	if n < 8 {
		n = 8
	}
	return [][]inlineBtn{{{Text: "↻ Blocks", CallbackData: fmt.Sprintf("r:b%d", n)}}}
}

func workerKeyboard() [][]inlineBtn {
	return [][]inlineBtn{{{Text: "↻ Worker", CallbackData: "r:k"}}}
}

func poolKeyboard() [][]inlineBtn {
	return [][]inlineBtn{{{Text: "↻ Pool", CallbackData: "r:p"}}}
}

func tasksKeyboard() [][]inlineBtn {
	return [][]inlineBtn{{{Text: "↻ Tasks", CallbackData: "r:t"}}}
}

func digestKeyboard() [][]inlineBtn {
	return [][]inlineBtn{{{Text: "↻ Digest", CallbackData: "r:d"}}}
}

func (b *bot) reply(ctx context.Context, chatID int64, html string, kb [][]inlineBtn) {
	_ = b.tgSendMessage(ctx, chatID, html, kb)
}

func (b *bot) handleCallback(ctx context.Context, cq *callbackQuery) {
	if cq.From == nil || cq.Message == nil {
		return
	}
	chatID := cq.Message.Chat.ID
	_ = b.tgAnswerCallback(ctx, cq.ID)
	if !b.allowed(cq.From.ID) {
		_ = b.tgSendMessage(ctx, chatID, deniedMsg(), nil)
		return
	}
	data := strings.TrimSpace(cq.Data)
	switch {
	case data == "r:s":
		b.reply(ctx, chatID, b.fmtStatus(ctx), statusKeyboard())
	case data == "r:m":
		b.reply(ctx, chatID, b.fmtMetrics(ctx), metricsKeyboard())
	case data == "r:w":
		b.reply(ctx, chatID, b.fmtWallet(ctx), walletKeyboard())
	case data == "r:d":
		b.reply(ctx, chatID, b.fmtDigest(ctx), digestKeyboard())
	case data == "r:k":
		b.reply(ctx, chatID, b.fmtWorker(ctx), workerKeyboard())
	case data == "r:p":
		b.reply(ctx, chatID, b.fmtPool(ctx), poolKeyboard())
	case data == "r:t":
		b.reply(ctx, chatID, b.fmtTasks(ctx), tasksKeyboard())
	case data == "r:a":
		b.reply(ctx, chatID, b.fmtAbout(ctx), mainKeyboard())
	case strings.HasPrefix(data, "r:b"):
		n := 8
		if v, err := strconv.Atoi(strings.TrimPrefix(data, "r:b")); err == nil && v > 0 {
			n = v
		}
		if n > 25 {
			n = 25
		}
		b.reply(ctx, chatID, b.fmtBlocks(ctx, n), blocksKeyboard(n))
	default:
		b.reply(ctx, chatID, helpText(b.cfg.nodeBase), mainKeyboard())
	}
}

// --- Node HTTP ---

func (b *bot) nodeGET(ctx context.Context, path string) (map[string]any, int, error) {
	u := b.cfg.nodeBase + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	if b.cfg.adminToken != "" {
		req.Header.Set("X-Hackme-Admin-Token", b.cfg.adminToken)
	}
	resp, err := b.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<22))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("json: %w body=%q", err, truncate(string(body), 200))
	}
	return m, resp.StatusCode, nil
}

func (b *bot) fmtAbout(ctx context.Context) string {
	st, code, err := b.nodeGET(ctx, "/api/status")
	if err != nil {
		return escHTML(fmt.Sprintf("Node error: %v", err))
	}
	if code != http.StatusOK {
		return escHTML(fmt.Sprintf("HTTP %d", code))
	}
	ver := asString(st["version"])
	cid := asString(st["chain_id"])
	commit := asString(st["commit"])
	return fmt.Sprintf("<b>HackMe node</b>\nchain <code>%s</code>\nversion <code>%s</code>\ncommit <code>%s</code>",
		escHTML(cid), escHTML(ver), escHTML(commit))
}

func (b *bot) fmtStatus(ctx context.Context) string {
	st, code, err := b.nodeGET(ctx, "/api/status")
	if err != nil {
		return escHTML(fmt.Sprintf("Node error: %v", err))
	}
	if code != http.StatusOK {
		return escHTML(fmt.Sprintf("HTTP %d", code))
	}
	var sb strings.Builder
	sb.WriteString("<b>Chain status</b>\n")
	sb.WriteString(fmtFlag("genesis", asBool(st["has_genesis"])))
	sb.WriteString(fmtField("tip_height", fmtUint(asFloat(st["tip_height"]))))
	sb.WriteString(fmtField("tip_hash", shortHash(asString(st["tip_hash"]))))
	sb.WriteString(fmtFlag("mining", asBool(st["mining"])))
	sb.WriteString(fmtField("node", asString(st["node_address"])))
	sb.WriteString(fmtFlag("network_mode", asBool(st["network_mode_active"])))
	if v, ok := st["canonical_tip_height"]; ok && asFloat(v) > 0 {
		sb.WriteString(fmtField("canonical_height", fmtUint(asFloat(v))))
		sb.WriteString(fmtField("canonical_hash", shortHash(asString(st["canonical_tip_hash"]))))
	}
	return sb.String()
}

func (b *bot) fmtWallet(ctx context.Context) string {
	w, code, err := b.nodeGET(ctx, "/api/wallet")
	if err != nil {
		return escHTML(fmt.Sprintf("Node error: %v", err))
	}
	if code != http.StatusOK {
		return escHTML(fmt.Sprintf("HTTP %d", code))
	}
	var sb strings.Builder
	sb.WriteString("<b>Wallet</b>\n")
	sb.WriteString(fmtField("address", asString(w["address"])))
	sb.WriteString(fmtField("balance_hmc", trimFloat(asString(w["balance_hmc"]))))
	sb.WriteString(fmtField("display_hmc", trimFloat(asString(w["balance_display_hmc"]))))
	sb.WriteString(fmtField("source", asString(w["wallet_source"])))
	sb.WriteString(fmtField("nonce", fmtUint(asFloat(w["next_nonce"]))))
	return sb.String()
}

func (b *bot) fmtWorker(ctx context.Context) string {
	ws, code, err := b.nodeGET(ctx, "/api/worker/status")
	if err != nil {
		return escHTML(fmt.Sprintf("Node error: %v", err))
	}
	if code != http.StatusOK {
		return escHTML(fmt.Sprintf("HTTP %d", code))
	}
	var sb strings.Builder
	sb.WriteString("<b>Pool worker</b>\n")
	sb.WriteString(fmtFlag("running", asBool(ws["running"])))
	sb.WriteString(fmtField("worker_id", asString(ws["worker_id"])))
	sb.WriteString(fmtField("coord_url", truncate(asString(ws["coord_url"]), 64)))
	sb.WriteString(fmtField("hashrate_GH/s", trimFloat(asString(ws["hashrate_gh_s"]))))
	sb.WriteString(fmtField("batch", fmtUint(asFloat(ws["batch_size"]))))
	sb.WriteString(fmtField("pid", fmtUint(asFloat(ws["pid"]))))

	setl, sc, _ := b.nodeGET(ctx, "/api/worker/settlement")
	if sc == http.StatusOK && setl != nil && asBool(setl["ok"]) {
		sb.WriteString(fmtField("unpaid_hmc", trimFloat(asString(setl["total_unpaid_hmc"]))))
	}
	return sb.String()
}

func (b *bot) fmtMetrics(ctx context.Context) string {
	m, code, err := b.nodeGET(ctx, "/api/metrics")
	if err != nil {
		return escHTML(fmt.Sprintf("Node error: %v", err))
	}
	if code != http.StatusOK {
		return escHTML(fmt.Sprintf("HTTP %d", code))
	}
	var sb strings.Builder
	sb.WriteString("<b>Mining telemetry</b>\n")
	sb.WriteString(fmtFlag("running", asBool(m["mining_running"])))
	sb.WriteString(fmtField("height", fmtUint(asFloat(m["block_height"]))))
	sb.WriteString(fmtField("backend", asString(m["mining_poh_backend"])))
	sb.WriteString(fmtField("target_mod", fmtUint(asFloat(m["mining_target_mod"]))))
	sb.WriteString(fmtField("attempts/s", trimFloat(asString(m["mining_attempts_per_sec"]))))
	sb.WriteString(fmtField("session_solves", fmtUint(asFloat(m["mining_session_solves"]))))
	sb.WriteString(fmtField("workers", fmtUint(asFloat(m["mining_workers"]))))
	sb.WriteString(fmtField("task", asString(m["mining_task_kind"])))
	sb.WriteString(fmtField("reward_next_hmc", trimFloat(asString(m["mining_reward_hmc"]))))
	sb.WriteString(fmtField("poh_blocks_1h", fmtUint(asFloat(m["mining_poh_blocks_last_1h"]))))
	sb.WriteString(fmtField("proj_hmc/h", trimFloat(asString(m["mining_projected_hmc_hour"]))))
	if s := asString(m["mining_insight_note"]); s != "" {
		sb.WriteString("\n<i>" + escHTML(truncate(s, 220)) + "</i>\n")
	}
	return sb.String()
}

func (b *bot) fmtDigest(ctx context.Context) string {
	st, _, _ := b.nodeGET(ctx, "/api/status")
	w, _, _ := b.nodeGET(ctx, "/api/wallet")
	m, _, _ := b.nodeGET(ctx, "/api/metrics")
	g, _, _ := b.nodeGET(ctx, "/api/global/metrics")
	var sb strings.Builder
	sb.WriteString("<b>HackMe digest</b>\n")
	if st != nil {
		sb.WriteString(fmtField("height", fmtUint(asFloat(st["tip_height"]))))
		sb.WriteString(fmtField("tip", shortHash(asString(st["tip_hash"]))))
		sb.WriteString(fmtFlag("mining", asBool(st["mining"])))
	}
	if g != nil {
		if network, ok := g["network"].(map[string]any); ok {
			poolGH := asFloat(network["pool_hashrate_gh_s"])
			if poolGH <= 0 {
				poolGH = asFloat(network["global_hashrate_th_s"]) * 1000
			}
			if poolGH > 0 {
				sb.WriteString(fmtField("pool", fmt.Sprintf("%.2f GH/s", poolGH)))
			}
			if rigs, ok := network["active_rigs"].([]any); ok {
				sb.WriteString(fmtField("rigs_online", fmt.Sprintf("%d", len(rigs))))
			}
		}
		if work, ok := g["work"].(map[string]any); ok {
			sb.WriteString(fmtField("target_M", fmtUint(asFloat(work["target_mod"]))))
		}
	}
	if w != nil {
		sb.WriteString(fmtField("balance", trimFloat(asString(w["balance_display_hmc"]))) + " HMC")
	}
	if m != nil {
		sb.WriteString(fmtField("local_attempts/s", trimFloat(asString(m["mining_attempts_per_sec"]))))
		sb.WriteString(fmtField("solves_sess", fmtUint(asFloat(m["mining_session_solves"]))))
	}
	return sb.String()
}

func (b *bot) fmtPool(ctx context.Context) string {
	g, code, err := b.nodeGET(ctx, "/api/global/metrics")
	if err != nil {
		return escHTML(fmt.Sprintf("Node error: %v", err))
	}
	if code != http.StatusOK {
		return escHTML(fmt.Sprintf("HTTP %d", code))
	}
	network, _ := g["network"].(map[string]any)
	work, _ := g["work"].(map[string]any)
	chain, _ := g["chain"].(map[string]any)
	var sb strings.Builder
	sb.WriteString("<b>Public pool</b>\n")
	if chain != nil {
		sb.WriteString(fmtField("tip_height", fmtUint(asFloat(chain["tip_height"]))))
	}
	if network != nil {
		poolGH := asFloat(network["pool_hashrate_gh_s"])
		if poolGH <= 0 {
			poolGH = asFloat(network["global_hashrate_th_s"]) * 1000
		}
		if poolGH > 0 {
			sb.WriteString(fmtField("pool_hashrate", fmt.Sprintf("%.2f GH/s", poolGH)))
		}
		if rigs, ok := network["active_rigs"].([]any); ok && len(rigs) > 0 {
			sb.WriteString("<b>Active rigs</b>\n")
			for i, it := range rigs {
				if i >= 8 {
					sb.WriteString(fmt.Sprintf("… +%d more\n", len(rigs)-8))
					break
				}
				row, ok := it.(map[string]any)
				if !ok {
					continue
				}
				wid := asString(row["worker_id"])
				if wid == "" {
					wid = asString(row["name"])
				}
				gh := asFloat(row["hashrate_gh_s"])
				sb.WriteString(fmt.Sprintf("• <code>%s</code> %.2f GH/s\n", escHTML(wid), gh))
			}
		}
	}
	if work != nil {
		sb.WriteString("\n<b>Coordinator</b>\n")
		sb.WriteString(fmtField("target_mod", fmtUint(asFloat(work["target_mod"]))))
		sb.WriteString(fmtField("workers", fmtUint(asFloat(work["workers_count"]))))
		sb.WriteString(fmtField("submitted", fmtUint(asFloat(work["submitted_items"]))))
		sb.WriteString(fmtField("attempts", fmtUint(asFloat(work["accepted_attempts"]))))
		if v := asFloat(work["total_payout_hmc"]); v > 0 {
			sb.WriteString(fmtField("pool_payout_hmc", trimFloat(fmt.Sprintf("%.8f", v))))
		}
	}
	if sb.Len() < 24 {
		sb.WriteString("<i>no pool data (coordinator offline?)</i>")
	}
	return strings.TrimSpace(sb.String())
}

func (b *bot) fmtTasks(ctx context.Context) string {
	j, code, err := b.nodeGET(ctx, "/api/tasks")
	if err != nil {
		return escHTML(fmt.Sprintf("Node error: %v", err))
	}
	if code != http.StatusOK {
		return escHTML(fmt.Sprintf("HTTP %d", code))
	}
	raw, _ := j["tasks"].([]any)
	open, done, cancelled := 0, 0, 0
	var sb strings.Builder
	sb.WriteString("<b>Orders / fuzzing tasks</b>\n")
	for _, it := range raw {
		row, ok := it.(map[string]any)
		if !ok {
			continue
		}
		switch strings.ToLower(asString(row["status"])) {
		case "open":
			open++
		case "completed":
			done++
		case "cancelled":
			cancelled++
		}
	}
	sb.WriteString(fmtField("open", fmt.Sprintf("%d", open)))
	sb.WriteString(fmtField("completed", fmt.Sprintf("%d", done)))
	sb.WriteString(fmtField("cancelled", fmt.Sprintf("%d", cancelled)))
	if open > 0 {
		sb.WriteString("\n<b>Open</b>\n")
		shown := 0
		for _, it := range raw {
			row, ok := it.(map[string]any)
			if !ok || strings.ToLower(asString(row["status"])) != "open" {
				continue
			}
			if shown >= 5 {
				break
			}
			id := truncate(asString(row["id"]), 28)
			reward := trimFloat(asString(row["reward"]))
			if reward == "" {
				reward = trimFloat(fmt.Sprintf("%.6f", asFloat(row["reward_hmc"])))
			}
			prog := fmtUint(asFloat(row["progress_count"]))
			target := fmtUint(asFloat(row["target_solves"]))
			kind := asString(row["kind"])
			sb.WriteString(fmt.Sprintf("• <code>%s</code>\n  %s · %s HMC · %s/%s solves\n",
				escHTML(id), escHTML(kind), escHTML(reward), prog, target))
			shown++
		}
	}
	if len(raw) == 0 {
		sb.WriteString("<i>no tasks loaded</i>")
	}
	return strings.TrimSpace(sb.String())
}

func (b *bot) fmtBlocks(ctx context.Context, limit int) string {
	path := fmt.Sprintf("/api/reports/blocks?limit=%d&source=auto", limit)
	j, code, err := b.nodeGET(ctx, path)
	if err != nil {
		return escHTML(fmt.Sprintf("Node error: %v", err))
	}
	if code != http.StatusOK {
		return escHTML(fmt.Sprintf("HTTP %d", code))
	}
	raw, _ := j["blocks"].([]any)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>Recent blocks</b> (last %d)\n", len(raw)))
	for i, it := range raw {
		if i >= limit {
			break
		}
		row, ok := it.(map[string]any)
		if !ok {
			continue
		}
		idx := fmtUint(asFloat(row["index"]))
		ts := int64(asFloat(row["timestamp_unix"]))
		kind := asString(row["task_kind"])
		h := shortHash(asString(row["hash_prefix"]))
		if h == "" {
			h = shortHash(asString(row["hash"]))
		}
		when := time.Unix(ts, 0).UTC().Format("2006-01-02 15:04")
		sb.WriteString(fmt.Sprintf("%s · h=%s · <code>%s</code> · %s\n", when, idx, escHTML(kind), escHTML(h)))
	}
	if sb.Len() < 40 {
		sb.WriteString("<i>no rows</i>")
	}
	return strings.TrimSpace(sb.String())
}

// --- Watchers ---

type watcher struct {
	cancel context.CancelFunc
	last   uint64
}

func (b *bot) startWatch(ctx context.Context, chatID int64) {
	b.stopWatch(chatID)
	ctxW, cancel := context.WithCancel(ctx)
	w := &watcher{cancel: cancel, last: 0}
	b.watches.Store(chatID, w)
	go b.watchLoop(ctxW, chatID, w)
	_ = b.tgSendMessage(ctx, chatID, escHTML(fmt.Sprintf("Alerts on: new block height (poll %ds). /unwatch to stop.", b.cfg.watchPollSec)), mainKeyboard())
}

func (b *bot) stopWatch(chatID int64) {
	if v, ok := b.watches.LoadAndDelete(chatID); ok {
		if w, ok := v.(*watcher); ok && w.cancel != nil {
			w.cancel()
		}
	}
}

func (b *bot) watchLoop(ctx context.Context, chatID int64, w *watcher) {
	tick := time.NewTicker(time.Duration(b.cfg.watchPollSec) * time.Second)
	defer tick.Stop()
	for {
		st, code, err := b.nodeGET(ctx, "/api/status")
		if err != nil || code != http.StatusOK {
			log.Printf("watch %d: status err=%v code=%d", chatID, err, code)
		} else {
			h := uint64(asFloat(st["tip_height"]))
			if w.last == 0 {
				w.last = h
			} else if h > w.last {
				msg := fmt.Sprintf("<b>New blocks</b>\nheight <code>%d</code> → <code>%d</code>", w.last, h)
				_ = b.tgSendMessage(context.Background(), chatID, msg, nil)
				w.last = h
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// --- Telegram API ---

const tgAPI = "https://api.telegram.org"

type inlineBtn struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

type callbackQuery struct {
	ID      string  `json:"id"`
	From    *tgUser `json:"from"`
	Message *struct {
		MessageID int64  `json:"message_id"`
		Chat      tgChat `json:"chat"`
		Text      string `json:"text"`
	} `json:"message"`
	Data string `json:"data"`
}

type tgUser struct {
	ID int64 `json:"id"`
}

type tgChat struct {
	ID int64 `json:"id"`
}

type tgUpdate struct {
	UpdateID      int64          `json:"update_id"`
	Message       *tgMessage     `json:"message"`
	CallbackQuery *callbackQuery `json:"callback_query"`
}

type tgMessage struct {
	MessageID int64   `json:"message_id"`
	From      *tgUser `json:"from"`
	Chat      tgChat  `json:"chat"`
	Text      string  `json:"text"`
}

func (b *bot) tgGetUpdates(ctx context.Context) ([]tgUpdate, error) {
	u := fmt.Sprintf("%s/bot%s/getUpdates?offset=%d&timeout=%d",
		tgAPI, b.cfg.token, b.offset, b.cfg.tgPollTimeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := b.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		OK     bool       `json:"ok"`
		Result []tgUpdate `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, errors.New("telegram ok=false")
	}
	return out.Result, nil
}

func (b *bot) tgSendMessage(ctx context.Context, chatID int64, html string, kb [][]inlineBtn) error {
	payload := map[string]any{
		"chat_id":                  chatID,
		"text":                     html,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	if len(kb) > 0 {
		payload["reply_markup"] = map[string]any{"inline_keyboard": kb}
	}
	return b.tgPostJSON(ctx, "sendMessage", payload)
}

func (b *bot) tgAnswerCallback(ctx context.Context, cqID string) error {
	payload := map[string]any{"callback_query_id": cqID}
	return b.tgPostJSON(ctx, "answerCallbackQuery", payload)
}

func (b *bot) tgPostJSON(ctx context.Context, method string, payload map[string]any) error {
	u := fmt.Sprintf("%s/bot%s/%s", tgAPI, b.cfg.token, method)
	buf, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var ack struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	_ = json.Unmarshal(body, &ack)
	if !ack.OK {
		return fmt.Errorf("telegram %s: %s", method, ack.Description)
	}
	return nil
}

func printCLIHelp() {
	fmt.Print(`HackMe Telegram operator bot — read-only GETs against your node's JSON API.

SETUP (each operator: own secrets, own node URL)
  1) Copy example:  cp scripts/ops/telegram_bot.env.example .env.telegram
  2) Set TELEGRAM_BOT_TOKEN (from @BotFather) and HACKME_TELEGRAM_NODE_URL (e.g. http://127.0.0.1:8080).
  3) Recommended: HACKME_TELEGRAM_ALLOWED_USER_IDS — comma-separated Telegram user ids.
  4) From repo root:  go run ./cmd/telegrambot
     or explicit file: go run ./cmd/telegrambot -config /path/to/operator.env

Environment (file or export; export wins over file):
  TELEGRAM_BOT_TOKEN or HACKME_TELEGRAM_BOT_TOKEN  — required
  HACKME_TELEGRAM_NODE_URL — node base URL (no trailing slash), default http://127.0.0.1:8080
  HACKME_TELEGRAM_ALLOWED_USER_IDS — optional allowlist of user ids
  HACKME_TELEGRAM_CONFIG — env file path if not using -config
  HACKME_ADMIN_TOKEN — optional, for nodes with admin auth on GET (rare)
  HACKME_TELEGRAM_WATCH_POLL_SEC — /watch interval, 20..600, default 45

Flags:
  -config path   load env file (error if missing)
  -help          this text

Default env files (if -config unset): first existing of .env.telegram, telegram_bot.env in cwd.
Do not commit tokens — see .gitignore (.env.telegram, telegram_bot.env).

Full guide: docs/TELEGRAM_BOT.md
`)
}

// --- Format helpers ---

func escHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func fmtField(k, v string) string {
	if v == "" {
		v = "—"
	}
	return fmt.Sprintf("%s: <code>%s</code>\n", k, escHTML(v))
}

func fmtFlag(k string, on bool) string {
	if on {
		return fmt.Sprintf("🟢 %s\n", escHTML(k))
	}
	return fmt.Sprintf("⚪ %s\n", escHTML(k))
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func asFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case json.Number:
		f, _ := t.Float64()
		return f
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case uint64:
		return float64(t)
	default:
		f, _ := strconv.ParseFloat(asString(v), 64)
		return f
	}
}

func asBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "true") || t == "1"
	default:
		return asFloat(v) != 0
	}
}

func fmtUint(f float64) string {
	if f < 0 {
		f = 0
	}
	return strconv.FormatUint(uint64(f+0.5), 10)
}

func trimFloat(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "0"
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return strconv.FormatFloat(f, 'f', 4, 64)
	}
	return s
}

func shortHash(hex string) string {
	hex = strings.TrimSpace(hex)
	if len(hex) <= 14 {
		return hex
	}
	return hex[:10] + "…" + hex[len(hex)-4:]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
