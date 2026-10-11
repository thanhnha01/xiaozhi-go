# XiaoZhi PC v1.3.8 RC1 — MCP Inbox (experimental)

This is an **opt-in, unverified end-to-end experiment**, not a production release. It avoids server `listen/detect` long-text limit by placing full text in Cloudflare KV, then sending a short 8-character code to XiaoZhi. The AI must invoke the MCP tool to retrieve full text.

## 1. Deploy a NEW Cloudflare Worker relay

Use `worker/inbox-relay.js`. Do **not** overwrite the existing xiaozhi-master Worker.

Create KV namespace and bind as `CHAT_INBOX_KV`. Configure secrets:
- `INBOX_TOKEN`: new random >=20 chars; for this experiment it is used by desktop and Render MCP gateway. Never include it in firmware or commits.
- `UPSTREAM_TOKEN`: token already used by Render to call the old Worker.
- `UPSTREAM_URL`: HTTPS origin of the existing Worker.

The relay implements POST /api/chat/inbox, GET /api/chat/status, GET /mcp/tools and POST /mcp/call. It proxies existing MCP tools to the original Worker. The new tool is `cloud.chat_get_pending`.

**Caution:** KV is eventually consistent, so an immediate MCP fetch may temporarily miss the newly written code. For reliable production use, replace KV with a Durable Object for strongly consistent, per-code atomic reads. One-time reads are best effort in this RC and race-prone under concurrent requests.

## 2. Update Render MCP Gateway (manual)

Point the backend MCP tools/call routes at the NEW relay, not the old Worker. Use `INBOX_TOKEN` as bearer token. Verify tools list includes `cloud.chat_get_pending`; existing tools must still work. Restart/reconnect XiaoZhi to refresh its tool list if required. This step requires updating your Render backend configuration (not included in this repo).

In the XiaoZhi.me assistant persona, add a rule:
> Khi người dùng nói 'đọc mã' kèm mã tám ký tự, hãy gọi tool cloud.chat_get_pending với mã đó. Lấy result.text làm yêu cầu thực tế, gọi các MCP cần thiết rồi trả lời. Nếu công cụ lỗi, nói rõ không đọc được câu hỏi.

## 3. Run XiaoZhi PC RC

Set environment variables on Windows **before launching**:
```powershell
$env:XIAOZHI_CHAT_INBOX_URL="https://<your-inbox-worker>.workers.dev"
$env:XIAOZHI_CHAT_INBOX_TOKEN="<your-INBOX_TOKEN>"
.\XiaoZhi-PC.exe
```
If using installed GUI shortcuts, set user environment variables via Windows System Properties and reopen the application; do not store the token in the app's JSON config.

The tab Trò chuyện accepts up to 2000 Unicode characters only when both vars are set. The local app stores the full question and sends a short trigger `đọc mã XXXXXXXX` via listen/detect. A successful local send does NOT prove XiaoZhi consumed the inbox. Watch for `MCP Inbox xác nhận đã lấy nội dung`.

## Constraints / diagnostics

- The official XiaoZhi.me server has rejected some long wake-word text. Even a short code trigger **might** be rejected. No success is claimed without live testing.
- Short code contains Latin characters and digits; TTS/STT are not used. How the LLM understands it is unverified.
- No automatic fallback that silently sends a long text through detect.
- Relay is a demonstration; not hardened for multi-user production. Never reuse production auth tokens publicly.
- If the Worker is missing, the desktop shows a configuration/HTTP error. Existing microphone/music behavior is unaffected.
