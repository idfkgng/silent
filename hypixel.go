package main

import (
	"bytes"
	"compress/zlib"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// ── CFB8 cipher (Minecraft uses AES-128-CFB8) ─────────────────────────────────

type cfb8Stream struct {
	block   cipher.Block
	sr      []byte // shift register = IV, 16 bytes
	decrypt bool
}

func newCFB8(key, iv []byte, decrypt bool) (cipher.Stream, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return &cfb8Stream{block: block, sr: append([]byte{}, iv...), decrypt: decrypt}, nil
}

func (c *cfb8Stream) XORKeyStream(dst, src []byte) {
	tmp := make([]byte, 16)
	for i := range src {
		c.block.Encrypt(tmp, c.sr)
		if c.decrypt {
			dst[i] = src[i] ^ tmp[0]
			copy(c.sr, c.sr[1:])
			c.sr[15] = src[i]
		} else {
			dst[i] = src[i] ^ tmp[0]
			copy(c.sr, c.sr[1:])
			c.sr[15] = dst[i]
		}
	}
}

// ── Minecraft VarInt / String / Packet helpers ────────────────────────────────

func writeVarInt(buf *bytes.Buffer, val int) {
	uv := uint32(val)
	for {
		b := byte(uv & 0x7F)
		uv >>= 7
		if uv != 0 {
			b |= 0x80
		}
		buf.WriteByte(b)
		if uv == 0 {
			break
		}
	}
}

func readVarInt(r io.Reader) (int, error) {
	var result int
	var shift uint
	one := make([]byte, 1)
	for {
		if _, err := io.ReadFull(r, one); err != nil {
			return 0, err
		}
		b := one[0]
		result |= int(b&0x7F) << shift
		if b&0x80 == 0 {
			return result, nil
		}
		shift += 7
		if shift >= 35 {
			return 0, fmt.Errorf("VarInt too large")
		}
	}
}

func writeString(buf *bytes.Buffer, s string) {
	b := []byte(s)
	writeVarInt(buf, len(b))
	buf.Write(b)
}

func readString(r io.Reader) (string, error) {
	n, err := readVarInt(r)
	if err != nil {
		return "", err
	}
	if n < 0 || n > 32767 {
		return "", fmt.Errorf("string length out of range: %d", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return string(b), nil
}

func readByteArray(r io.Reader) ([]byte, error) {
	n, err := readVarInt(r)
	if err != nil {
		return nil, err
	}
	if n < 0 || n > 1<<20 {
		return nil, fmt.Errorf("byte array length out of range: %d", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, err
	}
	return b, nil
}

// ── Connection state (handles encryption + compression) ───────────────────────

type mcConn struct {
	conn       net.Conn
	reader     io.Reader
	writer     io.Writer
	compressed bool
	threshold  int
}

func newMCConn(conn net.Conn) *mcConn {
	return &mcConn{conn: conn, reader: conn, writer: conn}
}

func (c *mcConn) enableEncryption(sharedSecret []byte) error {
	encStream, err := newCFB8(sharedSecret, sharedSecret, false)
	if err != nil {
		return err
	}
	decStream, err := newCFB8(sharedSecret, sharedSecret, true)
	if err != nil {
		return err
	}
	c.reader = cipher.StreamReader{S: decStream, R: c.conn}
	c.writer = cipher.StreamWriter{S: encStream, W: c.conn}
	return nil
}

func (c *mcConn) sendPacket(packetID int, data []byte) error {
	var pkt bytes.Buffer
	writeVarInt(&pkt, packetID)
	pkt.Write(data)
	pktBytes := pkt.Bytes()

	var frame bytes.Buffer
	if c.compressed {
		if len(pktBytes) >= c.threshold {
			// Compress
			var compressed bytes.Buffer
			w := zlib.NewWriter(&compressed)
			_, _ = w.Write(pktBytes)
			_ = w.Close()
			var payload bytes.Buffer
			writeVarInt(&payload, len(pktBytes)) // uncompressed length
			payload.Write(compressed.Bytes())
			writeVarInt(&frame, payload.Len())
			frame.Write(payload.Bytes())
		} else {
			// Send uncompressed with data length = 0
			var payload bytes.Buffer
			writeVarInt(&payload, 0)
			payload.Write(pktBytes)
			writeVarInt(&frame, payload.Len())
			frame.Write(payload.Bytes())
		}
	} else {
		writeVarInt(&frame, len(pktBytes))
		frame.Write(pktBytes)
	}

	_, err := c.writer.Write(frame.Bytes())
	return err
}

func (c *mcConn) readPacket() (int, []byte, error) {
	length, err := readVarInt(c.reader)
	if err != nil {
		return 0, nil, err
	}
	if length <= 0 || length > 1<<20 {
		return 0, nil, fmt.Errorf("invalid packet length: %d", length)
	}

	raw := make([]byte, length)
	if _, err := io.ReadFull(c.reader, raw); err != nil {
		return 0, nil, err
	}

	var payload []byte
	if c.compressed {
		buf := bytes.NewReader(raw)
		dataLen, err := readVarInt(buf)
		if err != nil {
			return 0, nil, err
		}
		rest := make([]byte, buf.Len())
		buf.Read(rest)
		if dataLen == 0 {
			payload = rest
		} else {
			r, err := zlib.NewReader(bytes.NewReader(rest))
			if err != nil {
				return 0, nil, err
			}
			payload, err = io.ReadAll(r)
			r.Close()
			if err != nil {
				return 0, nil, err
			}
		}
	} else {
		payload = raw
	}

	pr := bytes.NewReader(payload)
	packetID, err := readVarInt(pr)
	if err != nil {
		return 0, nil, err
	}
	remaining := make([]byte, pr.Len())
	pr.Read(remaining)
	return packetID, remaining, nil
}

// ── Minecraft SHA1 hash (Java-style, can be negative) ────────────────────────

func minecraftSHA1Hash(serverID string, sharedSecret, publicKey []byte) string {
	h := sha1.New()
	h.Write([]byte(serverID))
	h.Write(sharedSecret)
	h.Write(publicKey)
	hash := h.Sum(nil)

	negative := (hash[0] & 0x80) != 0
	if negative {
		// Two's complement
		carry := true
		for i := len(hash) - 1; i >= 0; i-- {
			hash[i] = ^hash[i]
			if carry {
				if hash[i] == 0xFF {
					hash[i] = 0x00
				} else {
					hash[i]++
					carry = false
				}
			}
		}
	}
	hex := fmt.Sprintf("%x", hash)
	hex = strings.TrimLeft(hex, "0")
	if hex == "" {
		hex = "0"
	}
	if negative {
		return "-" + hex
	}
	return hex
}

// ── Mojang session authentication ────────────────────────────────────────────

func mojangSessionAuth(accessToken, uuid, serverHash string) error {
	payload := map[string]string{
		"accessToken":     accessToken,
		"selectedProfile": strings.ReplaceAll(uuid, "-", ""),
		"serverId":        serverHash,
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(
		"https://sessionserver.mojang.com/session/minecraft/join",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != 204 {
		return fmt.Errorf("session auth returned %d", resp.StatusCode)
	}
	return nil
}

// ── Parse Hypixel disconnect message (exact same logic as bot.py) ─────────────

func parseHypixelDisconnect(jsonStr string) string {
	// Sometimes the JSON is double-encoded as a JSON string
	jsonStr = strings.TrimSpace(jsonStr)
	if strings.HasPrefix(jsonStr, `"`) {
		var s string
		if err := json.Unmarshal([]byte(jsonStr), &s); err == nil {
			jsonStr = s
		}
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return "Banned"
	}

	ds := jsonStr

	if strings.Contains(ds, "temporarily banned") {
		extras := extractExtras(data)
		dur, bid := "", ""
		if len(extras) > 4 {
			dur = strings.TrimSpace(extras[4])
		}
		if len(extras) > 8 {
			bid = strings.TrimSpace(extras[8])
		}
		if dur != "" {
			return fmt.Sprintf("[Temp] %s | ID: %s", dur, bid)
		}
		return "Temporarily Banned"
	}

	lower := strings.ToLower(ds)
	if strings.Contains(lower, "permanently banned") || strings.Contains(ds, "You are permanently") {
		extras := extractExtras(data)
		reason, bid := "", ""
		if len(extras) > 2 {
			reason = strings.TrimSpace(extras[2])
		}
		if len(extras) > 6 {
			bid = strings.TrimSpace(extras[6])
		}
		if reason != "" {
			return fmt.Sprintf("[Perm] %s | ID: %s", reason, bid)
		}
		return "[Permanently] Banned"
	}

	if strings.Contains(ds, "Suspicious activity") {
		return "[Perm] Suspicious Activity"
	}

	if strings.Contains(lower, "closed") || strings.Contains(lower, "cloning") {
		return "Unbanned"
	}

	// Fallback: join all extra text
	extras := extractExtras(data)
	if len(extras) > 0 {
		msg := strings.TrimSpace(strings.Join(extras, ""))
		if msg != "" {
			return msg
		}
	}
	if text, ok := data["text"].(string); ok && text != "" {
		return text
	}
	return "Banned"
}

func extractExtras(data map[string]interface{}) []string {
	raw, ok := data["extra"].([]interface{})
	if !ok {
		return nil
	}
	var out []string
	for _, e := range raw {
		if m, ok := e.(map[string]interface{}); ok {
			if t, ok := m["text"].(string); ok {
				out = append(out, t)
			}
		}
	}
	return out
}

// ── Main ban check — connects to mc.hypixel.net and checks disconnect ─────────

func CheckHypixelBan(r *AuthResult) {
	if r.MCToken == "" || r.UUID == "" || r.UUID == "N/A" ||
		r.Username == "" || r.Username == "N/A" {
		return
	}

	conn, err := net.DialTimeout("tcp", "mc.hypixel.net:25565", 10*time.Second)
	if err != nil {
		r.HypixelBan = "[Error] Connection Failed"
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))

	mc := newMCConn(conn)

	// ── Handshake (protocol 47 = 1.8.9, same as bot.py) ─────────────────────
	var hs bytes.Buffer
	writeVarInt(&hs, 47)
	writeString(&hs, "mc.hypixel.net")
	_ = binary.Write(&hs, binary.BigEndian, uint16(25565))
	writeVarInt(&hs, 2) // next state = login
	if err := mc.sendPacket(0x00, hs.Bytes()); err != nil {
		r.HypixelBan = "[Error] Handshake"
		return
	}

	// ── Login Start ──────────────────────────────────────────────────────────
	var ls bytes.Buffer
	writeString(&ls, r.Username)
	if err := mc.sendPacket(0x00, ls.Bytes()); err != nil {
		r.HypixelBan = "[Error] Login Start"
		return
	}

	// ── Read server response ─────────────────────────────────────────────────
	packetID, data, err := mc.readPacket()
	if err != nil {
		r.HypixelBan = "[Error] Read Response"
		return
	}

	// Might get immediate disconnect (maintenance, IP blocked, etc.)
	if packetID == 0x00 {
		dr := bytes.NewReader(data)
		reason, _ := readString(dr)
		result := parseHypixelDisconnect(reason)
		setHypixelBanResult(r, result)
		return
	}

	if packetID != 0x01 {
		r.HypixelBan = fmt.Sprintf("[Error] Unexpected packet 0x%02X", packetID)
		return
	}

	// ── Parse Encryption Request (0x01) ──────────────────────────────────────
	er := bytes.NewReader(data)
	serverID, err := readString(er)
	if err != nil {
		r.HypixelBan = "[Error] Parse Server ID"
		return
	}
	publicKeyBytes, err := readByteArray(er)
	if err != nil {
		r.HypixelBan = "[Error] Parse Public Key"
		return
	}
	verifyToken, err := readByteArray(er)
	if err != nil {
		r.HypixelBan = "[Error] Parse Verify Token"
		return
	}

	// ── Generate shared secret ────────────────────────────────────────────────
	sharedSecret := make([]byte, 16)
	if _, err := rand.Read(sharedSecret); err != nil {
		r.HypixelBan = "[Error] Generate Secret"
		return
	}

	// Parse RSA public key
	pub, err := x509.ParsePKIXPublicKey(publicKeyBytes)
	if err != nil {
		r.HypixelBan = "[Error] Parse RSA Key"
		return
	}
	rsaKey, ok := pub.(*rsa.PublicKey)
	if !ok {
		r.HypixelBan = "[Error] Not RSA Key"
		return
	}

	encSecret, err := rsa.EncryptPKCS1v15(rand.Reader, rsaKey, sharedSecret)
	if err != nil {
		r.HypixelBan = "[Error] Encrypt Secret"
		return
	}
	encToken, err := rsa.EncryptPKCS1v15(rand.Reader, rsaKey, verifyToken)
	if err != nil {
		r.HypixelBan = "[Error] Encrypt Token"
		return
	}

	// ── Mojang session auth ───────────────────────────────────────────────────
	serverHash := minecraftSHA1Hash(serverID, sharedSecret, publicKeyBytes)
	if err := mojangSessionAuth(r.MCToken, r.UUID, serverHash); err != nil {
		r.HypixelBan = "[Error] Session Auth"
		return
	}

	// ── Send Encryption Response (0x01) ──────────────────────────────────────
	var encResp bytes.Buffer
	writeVarInt(&encResp, len(encSecret))
	encResp.Write(encSecret)
	writeVarInt(&encResp, len(encToken))
	encResp.Write(encToken)
	if err := mc.sendPacket(0x01, encResp.Bytes()); err != nil {
		r.HypixelBan = "[Error] Send Encryption"
		return
	}

	// ── Enable encryption immediately ─────────────────────────────────────────
	if err := mc.enableEncryption(sharedSecret); err != nil {
		r.HypixelBan = "[Error] Enable Encryption"
		return
	}

	// ── Read response: Set Compression, Login Success, or Disconnect ──────────
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	for {
		packetID, data, err = mc.readPacket()
		if err != nil {
			r.HypixelBan = "[Error] Read Login Response"
			return
		}

		switch packetID {
		case 0x00: // Disconnect
			dr := bytes.NewReader(data)
			reason, _ := readString(dr)
			result := parseHypixelDisconnect(reason)
			setHypixelBanResult(r, result)
			return

		case 0x02: // Login Success = not banned
			r.HypixelBan = "Unbanned"
			AppendResult("HypixelUnbanned.txt", r.Email+":"+r.Password)
			return

		case 0x03: // Set Compression
			cr := bytes.NewReader(data)
			threshold, err := readVarInt(cr)
			if err != nil {
				r.HypixelBan = "[Error] Set Compression"
				return
			}
			if threshold >= 0 {
				mc.compressed = true
				mc.threshold = threshold
			}
			// Continue reading next packet

		default:
			// Unknown packet, keep reading
		}
	}
}

func setHypixelBanResult(r *AuthResult, result string) {
	r.HypixelBan = result
	if result == "Unbanned" {
		AppendResult("HypixelUnbanned.txt", r.Email+":"+r.Password)
	} else {
		AppendResult("HypixelBanned.txt", r.Email+":"+r.Password+" | "+result)
	}
}
