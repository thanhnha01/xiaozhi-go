// Cloudflare Worker — XiaoZhi MCP Inbox experimental relay.
// Deploy separately from your existing Worker. Requires KV binding CHAT_INBOX_KV,
// secrets INBOX_TOKEN and UPSTREAM_TOKEN, UPSTREAM_URL pointing to original Worker.
// Configure Render MCP gateway to call this relay for /mcp/tools and /mcp/call.
// ALL endpoints require Bearer INBOX_TOKEN. Never log tokens or full chat text.
const TTL=600;
function json(value,status=200){return new Response(JSON.stringify(value),{status,headers:{"Content-Type":"application/json; charset=utf-8","Cache-Control":"no-store","X-Content-Type-Options":"nosniff"}})}
function checkToken(req,env){return typeof env.INBOX_TOKEN==="string"&&env.INBOX_TOKEN.length>=20&&req.headers.get("Authorization")==="Bearer "+env.INBOX_TOKEN}
function code(){const alphabet="ABCDEFGHJKLMNPQRSTUVWXYZ23456789";const b=new Uint8Array(8);crypto.getRandomValues(b);return Array.from(b,n=>alphabet[n%alphabet.length]).join("")}
function validCode(v){return typeof v==="string"&&/^[A-HJ-NP-Z2-9]{8}$/.test(v)}
function tool(){return {name:"cloud.chat_get_pending",description:"Đọc nguyên văn câu hỏi dài mà XiaoZhi PC vừa gửi vào hộp thư. Nếu người dùng nói 'đọc mã XXXXXXXX', bắt buộc gọi công cụ này với mã chính xác rồi trả lời câu hỏi trong result.text. Không tiết lộ mã, không bịa nội dung nếu không tìm thấy. Chỉ có thể lấy một lần.",inputSchema:{type:"object",properties:{code:{type:"string",description:"Mã 8 ký tự đọc được trong lệnh đánh thức"}},required:["code"],additionalProperties:false}}}
async function upstream(req,env,method,path,body){
 if(!env.UPSTREAM_URL||!env.UPSTREAM_TOKEN) return json({ok:false,error:{message:"UPSTREAM_URL/UPSTREAM_TOKEN chưa cấu hình"}},503)
 const base=new URL(env.UPSTREAM_URL)
 if(base.protocol!=="https:")return json({ok:false,error:{message:"UPSTREAM_URL phải dùng HTTPS"}},503)
 const target=new URL(path,base.origin)
 const headers={"Authorization":"Bearer "+env.UPSTREAM_TOKEN}
 if(method==="POST")headers["Content-Type"]="application/json"
 const answer=await fetch(target.toString(),{method,headers,body:method==="POST"?JSON.stringify(body):undefined,redirect:"manual"})
 const b=await answer.text()
 return new Response(b,{status:answer.status,headers:{"Content-Type":"application/json; charset=utf-8","Cache-Control":"no-store"}})
}
async function main(req,env){
 if(!checkToken(req,env))return json({ok:false,error:{message:"UNAUTHORIZED"}},401)
 if(!env.CHAT_INBOX_KV)return json({ok:false,error:{message:"CHAT_INBOX_KV chưa được gắn"}},503)
 const url=new URL(req.url),p=url.pathname
 if(p==="/health"&&req.method==="GET")return json({ok:true,service:"xiaozhi-mcp-inbox-rc1",ttl_seconds:TTL})
 if(p==="/api/chat/inbox"&&req.method==="POST"){
  if(Number(req.headers.get("Content-Length")||0)>8192)return json({ok:false,error:{message:"Payload quá lớn"}},413)
  const raw=await req.text();if(raw.length>8192)return json({ok:false,error:{message:"Payload quá lớn"}},413)
  let input;try{input=JSON.parse(raw)}catch{return json({ok:false,error:{message:"JSON không hợp lệ"}},400)}
  const value=typeof input.text==="string"?input.text.trim():""
  if(!value||Array.from(value).length>2000||value.includes("\u0000"))return json({ok:false,error:{message:"Câu hỏi cần từ 1 đến 2000 ký tự"}},400)
  let id=code()
  for(let i=0;i<3;i++){if(await env.CHAT_INBOX_KV.get("c:"+id)===null)break;id=code()}
  await env.CHAT_INBOX_KV.put("c:"+id,JSON.stringify({text:value,created_at:Date.now(),status:"pending"}),{expirationTtl:TTL})
  await env.CHAT_INBOX_KV.put("s:"+id,"pending",{expirationTtl:TTL})
  return json({ok:true,code:id,expires_in:TTL})
 }
 if(p==="/api/chat/status"&&req.method==="GET"){
  const id=url.searchParams.get("code")
  if(!validCode(id))return json({ok:false,error:{message:"Mã không hợp lệ"}},400)
  return json({ok:true,status:await env.CHAT_INBOX_KV.get("s:"+id)||"expired"})
 }
 if(p==="/mcp/tools"&&req.method==="GET"){
  const origin=await upstream(req,env,"GET","/mcp/tools")
  if(!origin.ok)return origin
  const data=await origin.json()
  if(Array.isArray(data.tools)){data.tools=[...data.tools,tool()];data.tool_count=data.tools.length}
  return json(data)
 }
 if(p==="/mcp/call"&&req.method==="POST"){
  let body;try{body=await req.json()}catch{return json({ok:false,error:{message:"JSON không hợp lệ"}},400)}
  if(body?.name!=="cloud.chat_get_pending")return upstream(req,env,"POST","/mcp/call",body)
  const id=body.arguments?.code
  if(!validCode(id))return json({ok:false,error:{message:"Mã yêu cầu không hợp lệ"}},400)
  const key="c:"+id
  const raw=await env.CHAT_INBOX_KV.get(key)
  if(!raw)return json({ok:false,error:{message:"Mã không tồn tại hoặc đã hết hạn"}},404)
  const entry=JSON.parse(raw)
  if(entry.status!=="pending")return json({ok:false,error:{message:"Câu hỏi đã được đọc"}},409)
  await env.CHAT_INBOX_KV.put(key,JSON.stringify({...entry,status:"fetched",text:""}),{expirationTtl:60})
  await env.CHAT_INBOX_KV.put("s:"+id,"fetched",{expirationTtl:TTL})
  // Compatible with the existing Worker mcpSuccess JSON envelope.
  return json({ok:true,schema:"xiaozhi.mcp.response.v1",tool:"cloud.chat_get_pending",version:"1.3.8-rc1",result:{status:"ready",text:entry.text,code:id},meta:{source:"chat_inbox",cached:false}})
 }
 return json({ok:false,error:{message:"NOT_FOUND"}},404)
}
export default {async fetch(req,env){try{return await main(req,env)}catch(err){return json({ok:false,error:{message:"Inbox internal error"}},500)}}};
