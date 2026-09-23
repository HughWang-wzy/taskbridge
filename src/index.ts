interface Env {
	DB: D1Database;
	TB_ADMIN_TOKEN: string;
	LOST_TIMEOUT_SECONDS?: string;
}

interface NotifyBody {
	title?: string;
	message?: string;
	priority?: number;
	tags?: string[];
}

interface StartTaskBody {
	id?: string;
	name?: string;
	kind?: string;
	host?: string;
	platform?: string;
	cwd?: string;
	command?: string;
	metadata?: unknown;
}

interface FinishTaskBody {
	exit_code?: number;
	error_summary?: string;
	metadata?: unknown;
}

interface TaskRow {
	id: string;
	client_id: string | null;
	name: string;
	kind: string | null;
	host: string | null;
	platform: string | null;
	cwd: string | null;
	command: string | null;
	status: string;
	created_at: number;
	started_at: number;
	last_heartbeat: number;
	finished_at: number | null;
	exit_code: number | null;
	duration_ms: number | null;
	metadata_json: string | null;
	error_summary: string | null;
}

function json(data: unknown, status = 200): Response {
	return new Response(JSON.stringify(data, null, 2), {
		status,
		headers: {
			"content-type": "application/json; charset=utf-8",
		},
	});
}

interface Auth { clientId: string | null; admin: boolean }

async function tokenHash(token: string): Promise<string> {
	const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(token));
	return Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, '0')).join('');
}

async function authorize(request: Request, env: Env, scope: string): Promise<Auth | null> {
	const header = request.headers.get('authorization');
	if (!header?.startsWith('Bearer ')) return null;
	const token = header.slice(7);
	if (env.TB_ADMIN_TOKEN && token === env.TB_ADMIN_TOKEN) return {clientId: null, admin: true};
	if (!token) return null;
	const client = await env.DB.prepare('SELECT id, scopes FROM clients WHERE token_hash = ? AND revoked_at IS NULL')
		.bind(await tokenHash(token)).first<{id: string; scopes: string}>();
	if (!client) return null;
	let scopes: string[] = [];
	try { scopes = JSON.parse(client.scopes) as string[] } catch { scopes = client.scopes.split(',') }
	return scopes.includes(scope) ? {clientId: client.id, admin: false} : null;
}

function ownsTask(auth: Auth, task: TaskRow): boolean {
	return auth.admin || auth.clientId === task.client_id;
}

function nowMs(): number {
	return Date.now();
}

function metadataToJson(value: unknown): string | null {
	if (value === undefined || value === null) {
		return null;
	}

	try {
		return JSON.stringify(value);
	} catch {
		return null;
	}
}

interface NotificationInput {
	title: string;
	message: string;
	priority?: number;
	tags?: string[];
	actions?: Array<Record<string, unknown>>;
}

async function queueNotification(env: Env, id: string, input: NotificationInput): Promise<boolean> {
	const now=nowMs();
	const result=await env.DB.prepare('INSERT OR IGNORE INTO notifications (id,payload_json,created_at,next_attempt_at) VALUES (?,?,?,?)')
		.bind(id,JSON.stringify(input),now,now).run();
	return result.meta.changes>0;
}


async function claimNotifications(env: Env, limit: number, requestedId?: string): Promise<Array<{id:string;claim_token:string;payload:NotificationInput}>> {
	const now=nowMs();
	const due=requestedId
		? await env.DB.prepare('SELECT id,payload_json FROM notifications WHERE id=? AND sent_at IS NULL AND next_attempt_at <= ? AND lease_until < ?')
			.bind(requestedId,now,now).all<{id:string;payload_json:string}>()
		: await env.DB.prepare('SELECT id,payload_json FROM notifications WHERE sent_at IS NULL AND next_attempt_at <= ? AND lease_until < ? ORDER BY created_at LIMIT ?')
			.bind(now,now,limit).all<{id:string;payload_json:string}>();
	const claimed: Array<{id:string;claim_token:string;payload:NotificationInput}>=[];
	for (const item of due.results) {
		const questionId=item.id.match(/^question:([^:]+):created$/)?.[1];
		if (questionId) {
			const question=await env.DB.prepare('SELECT status,expires_at FROM questions WHERE id=?').bind(questionId).first<{status:string;expires_at:number}>();
			if (!question || question.status!=='pending' || question.expires_at<nowMs()) {
				await env.DB.prepare('DELETE FROM notifications WHERE id=? AND sent_at IS NULL').bind(item.id).run();
				continue;
			}
		}
		const claimToken=crypto.randomUUID();
		const result=await env.DB.prepare('UPDATE notifications SET lease_until=?,claim_token=? WHERE id=? AND sent_at IS NULL AND lease_until < ? AND next_attempt_at <= ?')
			.bind(now+120000,claimToken,item.id,now,now).run();
		if (result.meta.changes) claimed.push({id:item.id,claim_token:claimToken,payload:JSON.parse(item.payload_json) as NotificationInput});
	}
	return claimed;
}

function randomSecret(): string {
	return Array.from(crypto.getRandomValues(new Uint8Array(32)), byte => byte.toString(16).padStart(2, '0')).join('');
}

function escapeHtml(value: string): string {
	return value.replace(/[&<>"']/g, char => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]!));
}

export default {
	async fetch(request, env): Promise<Response> {
		const url = new URL(request.url);
		if (request.method==='POST' && url.pathname==='/v1/notifications') {
			const auth=await authorize(request,env,'notify:write');
			if (!auth) return json({ok:false,error:'unauthorized'},401);
			let body: {id?:string;payload?:NotificationInput};
			try {body=await request.json() as typeof body} catch {return json({ok:false,error:'invalid_json'},400)}
			if (!body.id || body.id.length>200 || !/^[a-zA-Z0-9:_-]+$/.test(body.id) ||
				!body.payload || typeof body.payload.title!=='string' || typeof body.payload.message!=='string' ||
				!body.payload.message.trim() || body.payload.title.length>200 || body.payload.message.length>3000) {
				return json({ok:false,error:'invalid_notification'},400);
			}
			const created=await queueNotification(env,body.id,body.payload);
			return json({ok:true,id:body.id,queued:true,idempotent:!created},created?201:200);
		}
		if (request.method==='POST' && url.pathname==='/v1/notifications/claim') {
			const auth=await authorize(request,env,'notifications:relay');
			if (!auth) return json({ok:false,error:'unauthorized'},401);
			let body: {limit?:number;id?:string}={};
			try {body=await request.json() as typeof body} catch {return json({ok:false,error:'invalid_json'},400)}
			if (body.id && (typeof body.id!=='string' || body.id.length>200)) return json({ok:false,error:'invalid_id'},400);
			const limit=Math.min(20,Math.max(1,Number(body.limit)||10));
			return json({ok:true,notifications:await claimNotifications(env,limit,body.id)});
		}
		const deliveryMatch=url.pathname.match(/^\/v1\/notifications\/([^/]+)\/(ack|fail)$/);
		if (request.method==='POST' && deliveryMatch) {
			const auth=await authorize(request,env,'notifications:relay');
			if (!auth) return json({ok:false,error:'unauthorized'},401);
			let body: {claim_token?:string};
			try {body=await request.json() as typeof body} catch {return json({ok:false,error:'invalid_json'},400)}
			const id=decodeURIComponent(deliveryMatch[1]);
			const row=await env.DB.prepare('SELECT sent_at,claim_token,lease_until,attempts FROM notifications WHERE id=?')
				.bind(id).first<{sent_at:number|null;claim_token:string|null;lease_until:number;attempts:number}>();
			if (!row) return json({ok:false,error:'not_found'},404);
			if (row.sent_at!==null) return json({ok:true,idempotent:true});
			if (!body.claim_token || body.claim_token!==row.claim_token || row.lease_until<nowMs()) return json({ok:false,error:'stale_claim'},409);
			if (deliveryMatch[2]==='ack') {
				const updated=await env.DB.prepare('UPDATE notifications SET sent_at=?,lease_until=0,claim_token=NULL WHERE id=? AND sent_at IS NULL AND claim_token=? AND lease_until>=?')
					.bind(nowMs(),id,body.claim_token,nowMs()).run();
				return updated.meta.changes ? json({ok:true}) : json({ok:false,error:'stale_claim'},409);
			}
			const delay=Math.min(3600,60*2**Math.min(row.attempts,6))*1000;
			const updated=await env.DB.prepare('UPDATE notifications SET attempts=attempts+1,next_attempt_at=?,lease_until=0,claim_token=NULL WHERE id=? AND sent_at IS NULL AND claim_token=? AND lease_until>=?')
				.bind(nowMs()+delay,id,body.claim_token,nowMs()).run();
			return updated.meta.changes ? json({ok:true,retry_after_ms:delay}) : json({ok:false,error:'stale_claim'},409);
		}
		if (request.method==='GET' && url.pathname==='/v1/doctor') {
			const auth=await authorize(request,env,'tasks:read');
			if (!auth) return json({ok:false,error:'unauthorized'},401);
			const row=await env.DB.prepare('SELECT COUNT(*) AS pending, SUM(CASE WHEN attempts > 0 THEN 1 ELSE 0 END) AS failed FROM notifications WHERE sent_at IS NULL')
				.first<{pending:number;failed:number|null}>();
			return json({ok:true,service:'taskbridge',delivery_mode:'client_relay',pending_notifications:row?.pending??0,failed_notification_attempts:row?.failed??0});
		}

		if (request.method === 'POST' && url.pathname === '/v1/codex/events') {
			const auth = await authorize(request,env,'codex:write');
			if (!auth) return json({ok:false,error:'unauthorized'},401);
			let body: {session_id?:string; turn_id?:string; event?:string};
			try {body=await request.json() as typeof body} catch {return json({ok:false,error:'invalid_json'},400)}
			if (!body.session_id || !body.turn_id || !['Stop','Interrupt','PostToolUse'].includes(body.event ?? '')) return json({ok:false,error:'invalid_event'},400);
			const bucket = body.event === 'PostToolUse' ? `:${Math.floor(nowMs()/120000)}` : '';
			const id = `codex:${body.session_id}:${body.turn_id}:${body.event}${bucket}`;
			const inserted = await env.DB.prepare('INSERT OR IGNORE INTO events (id,client_id,event_type,payload_json,created_at) VALUES (?,?,?,?,?)')
				.bind(id,auth.clientId,`codex.${body.event}`,metadataToJson(body),nowMs()).run();
			if (!inserted.meta.changes) return json({ok:true,duplicate:true});
			return json({ok:true,duplicate:false});
		}

		if (request.method === 'POST' && url.pathname === '/v1/questions') {
			const auth = await authorize(request,env,'questions:write');
			if (!auth) return json({ok:false,error:'unauthorized'},401);
			let body: {question_type?:string; question?:string; options?:string[]; session_id?:string; turn_id?:string; task_id?:string; timeout_seconds?:number};
			try {body=await request.json() as typeof body} catch {return json({ok:false,error:'invalid_json'},400)}
			const type=body.question_type;
			if (!['confirm','choice','text'].includes(type ?? '') || !body.question?.trim() || body.question.length>3000) return json({ok:false,error:'invalid_question'},400);
			const options=type==='confirm' ? ['yes','no'] : type==='choice' ? body.options : [];
			if (type==='choice' && (!Array.isArray(options) || options.length<2 || options.length>10 || options.some(option=>typeof option!=='string'||!option.trim()||option.length>100))) return json({ok:false,error:'invalid_options'},400);
			const id=crypto.randomUUID(), secret=randomSecret();
			const timeout=Math.min(86400,Math.max(60,Number(body.timeout_seconds)||3600));
			await env.DB.prepare('INSERT INTO questions (id,client_id,task_id,session_id,turn_id,question_type,question,options_json,status,created_at,expires_at,answer_token_hash) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)')
				.bind(id,auth.clientId,body.task_id??null,body.session_id??null,body.turn_id??null,type,body.question.trim(),metadataToJson(options),'pending',nowMs(),nowMs()+timeout*1000,await tokenHash(secret)).run();
			const answerURL=`${url.origin}/q/${id}?token=${secret}`;
			const actions: Array<Record<string,unknown>> = [];
			if (options && options.length<=3 && type!=='text') {
				for (const option of options) actions.push({action:'http',label:option,url:`${url.origin}/v1/questions/${id}/answer?token=${secret}`,method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({answer:option}),clear:true});
			} else actions.push({action:'view',label:'Answer',url:answerURL});
			await queueNotification(env,`question:${id}:created`,{title:'❓ Codex needs your answer',message:body.question.trim(),priority:4,actions});
			return json({ok:true,id,answer_url:answerURL,expires_at:nowMs()+timeout*1000},201);
		}

		const questionMatch=url.pathname.match(/^\/v1\/questions\/([^/]+)$/);
		if (request.method==='GET' && questionMatch) {
			const auth=await authorize(request,env,'questions:read');
			if (!auth) return json({ok:false,error:'unauthorized'},401);
			const question=await env.DB.prepare('SELECT id,client_id,question_type,question,options_json,status,answer,created_at,expires_at,answered_at FROM questions WHERE id=?').bind(questionMatch[1]).first<{client_id:string|null;status:string;expires_at:number}>();
			if (!question || (!auth.admin && question.client_id!==auth.clientId)) return json({ok:false,error:'question_not_found'},404);
			if (question.status==='pending' && question.expires_at<nowMs()) {
				await env.DB.prepare("UPDATE questions SET status='expired' WHERE id=? AND status='pending'").bind(questionMatch[1]).run();
				question.status='expired';
			}
			return json({ok:true,question});
		}

		const answerMatch=url.pathname.match(/^\/v1\/questions\/([^/]+)\/answer$/);
		const desktopAnswerMatch=url.pathname.match(/^\/v1\/questions\/([^/]+)\/answer-desktop$/);
		if (request.method==='POST' && desktopAnswerMatch) {
			const auth=await authorize(request,env,'questions:write');
			if (!auth) return json({ok:false,error:'unauthorized'},401);
			const question=await env.DB.prepare('SELECT client_id,question_type,options_json,status,answer,expires_at FROM questions WHERE id=?')
				.bind(desktopAnswerMatch[1]).first<{client_id:string|null;question_type:string;options_json:string|null;status:string;answer:string|null;expires_at:number}>();
			if (!question || (!auth.admin && question.client_id!==auth.clientId)) return json({ok:false,error:'question_not_found'},404);
			let body: {answer?:string};
			try {body=await request.json() as typeof body} catch {return json({ok:false,error:'invalid_json'},400)}
			const answer=typeof body.answer==='string' ? body.answer.trim() : '';
			const options=question.options_json ? JSON.parse(question.options_json) as string[] : [];
			if (!answer || answer.length>3000 || (question.question_type!=='text' && !options.includes(answer))) return json({ok:false,error:'invalid_answer'},400);
			if (question.status==='answered') return json({ok:true,duplicate:true,answer:question.answer});
			if (question.expires_at<nowMs()) return json({ok:false,error:'expired'},410);
			const updated=await env.DB.prepare("UPDATE questions SET status='answered',answer=?,answered_at=? WHERE id=? AND status='pending' AND expires_at>=?")
				.bind(answer,nowMs(),desktopAnswerMatch[1],nowMs()).run();
			if (updated.meta.changes) return json({ok:true,duplicate:false,answer});
			const winner=await env.DB.prepare('SELECT answer,status FROM questions WHERE id=?').bind(desktopAnswerMatch[1]).first<{answer:string|null;status:string}>();
			return winner?.status==='answered' ? json({ok:true,duplicate:true,answer:winner.answer}) : json({ok:false,error:'expired'},410);
		}
		if (request.method==='OPTIONS' && answerMatch) return new Response(null,{status:204,headers:{'access-control-allow-origin':'https://ntfy.sh','access-control-allow-methods':'POST','access-control-allow-headers':'content-type'}});
		if (request.method==='POST' && answerMatch) {
			const question=await env.DB.prepare('SELECT answer_token_hash,question_type,options_json,status,expires_at FROM questions WHERE id=?').bind(answerMatch[1]).first<{answer_token_hash:string;question_type:string;options_json:string|null;status:string;expires_at:number}>();
			if (!question || !url.searchParams.get('token') || await tokenHash(url.searchParams.get('token')!)!==question.answer_token_hash) return json({ok:false,error:'not_found'},404);
			let answer='';
			if (request.headers.get('content-type')?.includes('application/json')) {try {const body=await request.json() as {answer?:string};answer=body.answer??''} catch {return json({ok:false,error:'invalid_json'},400)}}
			else {const form=await request.formData();answer=String(form.get('answer')??'')}
			answer=answer.trim();
			const options=question.options_json ? JSON.parse(question.options_json) as string[] : [];
			if (!answer || answer.length>3000 || (question.question_type!=='text' && !options.includes(answer))) return json({ok:false,error:'invalid_answer'},400);
			if (question.status==='answered') return new Response(JSON.stringify({ok:true,duplicate:true}),{headers:{'content-type':'application/json','access-control-allow-origin':'https://ntfy.sh'}});
			if (question.expires_at<nowMs()) return json({ok:false,error:'expired'},410);
			const updated=await env.DB.prepare("UPDATE questions SET status='answered',answer=?,answered_at=? WHERE id=? AND status='pending'").bind(answer,nowMs(),answerMatch[1]).run();
			return new Response(JSON.stringify({ok:true,duplicate:!updated.meta.changes}),{headers:{'content-type':'application/json','access-control-allow-origin':'https://ntfy.sh'}});
		}

		const pageMatch=url.pathname.match(/^\/q\/([^/]+)$/);
		if (request.method==='GET' && pageMatch) {
			const question=await env.DB.prepare('SELECT question,question_type,options_json,answer_token_hash,status,expires_at FROM questions WHERE id=?').bind(pageMatch[1]).first<{question:string;question_type:string;options_json:string|null;answer_token_hash:string;status:string;expires_at:number}>();
			if (!question || !url.searchParams.get('token') || await tokenHash(url.searchParams.get('token')!)!==question.answer_token_hash) return new Response('Not found',{status:404});
			if (question.status!=='pending' || question.expires_at<nowMs()) return new Response('This question is closed.',{status:410});
			const options=question.options_json ? JSON.parse(question.options_json) as string[] : [];
			const field=question.question_type==='text' ? '<textarea name="answer" required maxlength="3000" rows="6"></textarea>' : `<select name="answer">${options.map(option=>`<option value="${escapeHtml(option)}">${escapeHtml(option)}</option>`).join('')}</select>`;
			const html=`<!doctype html><html lang="en"><meta name="viewport" content="width=device-width,initial-scale=1"><meta charset="utf-8"><title>TaskBridge answer</title><style>body{font:18px system-ui;max-width:600px;margin:3rem auto;padding:1rem;color:#17202a}textarea,select,button{display:block;width:100%;box-sizing:border-box;font:inherit;margin:1rem 0;padding:.7rem}button{background:#155eef;color:white;border:0;border-radius:8px}</style><h1>Codex needs your answer</h1><p>${escapeHtml(question.question)}</p><form method="post" action="/v1/questions/${encodeURIComponent(pageMatch[1])}/answer?token=${encodeURIComponent(url.searchParams.get('token')!)}">${field}<button>Send answer</button></form></html>`;
			return new Response(html,{headers:{'content-type':'text/html; charset=utf-8','content-security-policy':"default-src 'none'; style-src 'unsafe-inline'; form-action 'self'"}});
		}

		if (request.method === 'POST' && url.pathname === '/v1/clients') {
			const admin = await authorize(request, env, 'admin');
			if (!admin?.admin) return json({ok:false,error:'unauthorized'},401);
			let body: {name?: string; scopes?: string[]};
			try { body = await request.json() as typeof body } catch { return json({ok:false,error:'invalid_json'},400) }
			const allowed = new Set(['tasks:write','tasks:read','notify:write','questions:write','questions:read','codex:write','notifications:relay']);
			if (!body.name?.trim() || !Array.isArray(body.scopes) || body.scopes.length === 0 || body.scopes.some(scope => !allowed.has(scope))) {
				return json({ok:false,error:'invalid_client'},400);
			}
			const id = crypto.randomUUID();
			const secret = randomSecret();
			await env.DB.prepare('INSERT INTO clients (id,name,token_hash,scopes,created_at) VALUES (?,?,?,?,?)')
				.bind(id,body.name.trim().slice(0,100),await tokenHash(secret),JSON.stringify([...new Set(body.scopes)]),nowMs()).run();
			return json({ok:true,id,name:body.name.trim(),token:secret,scopes:body.scopes},201);
		}

		// =====================================================
		// GET /health
		// =====================================================
		if (request.method === "GET" && url.pathname === "/health") {
			try {
				const result = await env.DB
					.prepare("SELECT 1 AS ok")
					.first<{ ok: number }>();

				return json({
					ok: true,
					service: "taskbridge",
					db: result?.ok === 1 ? "ok" : "unknown",
					time: new Date().toISOString(),
				});
			} catch (error) {
				return json(
					{
						ok: false,
						service: "taskbridge",
						db: "error",
						error:
							error instanceof Error
								? error.message
								: String(error),
					},
					500,
				);
			}
		}

		// =====================================================
		// POST /v1/notify
		// 手动通知
		// =====================================================
		if (
			request.method === "POST" &&
			url.pathname === "/v1/notify"
		) {
			const auth = await authorize(request, env, "notify:write");
			if (!auth) {
				return json({ ok: false, error: "unauthorized" }, 401);
			}

			let body: NotifyBody;

			try {
				body = (await request.json()) as NotifyBody;
			} catch {
				return json({ ok: false, error: "invalid_json" }, 400);
			}

			const title =
				typeof body.title === "string"
					? body.title.slice(0, 200)
					: "TaskBridge";

			const message =
				typeof body.message === "string"
					? body.message.slice(0, 3000)
					: "";

			if (!message) {
				return json(
					{ ok: false, error: "message_required" },
					400,
				);
			}

			const priority =
				Number.isInteger(body.priority) &&
				body.priority! >= 1 &&
				body.priority! <= 5
					? body.priority!
					: 3;

			const tags = Array.isArray(body.tags)
				? body.tags
						.filter(
							(value): value is string =>
								typeof value === "string",
						)
						.slice(0, 5)
				: [];

			const id=`manual:${crypto.randomUUID()}`;
			await queueNotification(env,id,{title,message,priority,tags});

			return json({
				ok: true,
				title,
				priority,
				id,
				queued:true,
			});
		}

		// =====================================================
		// POST /v1/tasks/start
		// =====================================================
		if (
			request.method === "POST" &&
			url.pathname === "/v1/tasks/start"
		) {
			const auth = await authorize(request, env, "tasks:write");
			if (!auth) {
				return json({ ok: false, error: "unauthorized" }, 401);
			}

			let body: StartTaskBody;

			try {
				body = (await request.json()) as StartTaskBody;
			} catch {
				return json({ ok: false, error: "invalid_json" }, 400);
			}

			if (!body.id || typeof body.id !== "string") {
				return json(
					{ ok: false, error: "task_id_required" },
					400,
				);
			}

			if (!body.name || typeof body.name !== "string") {
				return json(
					{ ok: false, error: "task_name_required" },
					400,
				);
			}

			const timestamp = nowMs();

			await env.DB
				.prepare(`
					INSERT OR IGNORE INTO tasks (
						id,
						client_id,
						name,
						kind,
						host,
						platform,
						cwd,
						command,
						status,
						created_at,
						started_at,
						last_heartbeat,
						metadata_json
					)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'running', ?, ?, ?, ?)
				`)
				.bind(
					body.id,
					auth.clientId,
					body.name.slice(0, 300),
					body.kind ?? null,
					body.host ?? null,
					body.platform ?? null,
					body.cwd ?? null,
					body.command ?? null,
					timestamp,
					timestamp,
					timestamp,
					metadataToJson(body.metadata),
				)
				.run();

			await env.DB
				.prepare(`
					INSERT OR IGNORE INTO events (
						id,
						task_id,
						event_type,
						payload_json,
						created_at
					)
					VALUES (?, ?, 'task.started', ?, ?)
				`)
				.bind(
					`task:${body.id}:started`,
					body.id,
					metadataToJson(body),
					timestamp,
				)
				.run();

			const task = await env.DB
				.prepare("SELECT * FROM tasks WHERE id = ?")
				.bind(body.id)
				.first<TaskRow>();
			if (task && !ownsTask(auth, task)) return json({ok:false,error:'forbidden'},403);

			return json(
				{
					ok: true,
					task,
				},
				201,
			);
		}

		// =====================================================
		// POST /v1/tasks/:id/heartbeat
		// =====================================================
		const heartbeatMatch = url.pathname.match(
			/^\/v1\/tasks\/([^/]+)\/heartbeat$/,
		);

		if (request.method === "POST" && heartbeatMatch) {
			const auth = await authorize(request, env, "tasks:write");
			if (!auth) {
				return json({ ok: false, error: "unauthorized" }, 401);
			}

			const taskId = heartbeatMatch[1];

			const task = await env.DB
				.prepare("SELECT * FROM tasks WHERE id = ?")
				.bind(taskId)
				.first<TaskRow>();

			if (!task) {
				return json({ ok: false, error: "task_not_found" }, 404);
			}
			if (!ownsTask(auth, task)) return json({ok:false,error:'forbidden'},403);

			if (
				task.status === "completed" ||
				task.status === "failed" ||
				task.status === "interrupted"
			) {
				return json({
					ok: true,
					ignored: true,
					status: task.status,
				});
			}

			const timestamp = nowMs();
			const recovery = await env.DB
				.prepare(`
					UPDATE tasks
					SET
						last_heartbeat = ?,
						status = 'running'
					WHERE id = ? AND status = 'lost'
				`)
				.bind(timestamp, taskId)
				.run();
			const recovered = recovery.meta.changes > 0;
			if (!recovered) {
				await env.DB.prepare("UPDATE tasks SET last_heartbeat = ? WHERE id = ? AND status = 'running'")
					.bind(timestamp, taskId).run();
			}

			if (recovered) {
				await env.DB.prepare("INSERT OR IGNORE INTO events (id,task_id,event_type,created_at) VALUES (?,?,'task.recovered',?)")
					.bind(`task:${taskId}:recovered:${timestamp}`,taskId,timestamp).run();
				try {
					await queueNotification(env,`task:${taskId}:recovered:${timestamp}`, {
						title: "🟢 Task recovered",
						message:
							`${task.name}\n\n` +
							`Host: ${task.host ?? "unknown"}\n` +
							`任务重新恢复心跳。`,
						priority: 3,
						tags: ["green_circle"],
					});
				} catch {
					// 不让通知失败影响 heartbeat
				}
			}

			return json({
				ok: true,
				task_id: taskId,
				status: "running",
				last_heartbeat: timestamp,
				recovered,
			});
		}

		// =====================================================
		// POST /v1/tasks/:id/finish
		// =====================================================
		const finishMatch = url.pathname.match(
			/^\/v1\/tasks\/([^/]+)\/finish$/,
		);

		if (request.method === "POST" && finishMatch) {
			const auth = await authorize(request, env, "tasks:write");
			if (!auth) {
				return json({ ok: false, error: "unauthorized" }, 401);
			}

			const taskId = finishMatch[1];

			let body: FinishTaskBody;

			try {
				body = (await request.json()) as FinishTaskBody;
			} catch {
				return json({ ok: false, error: "invalid_json" }, 400);
			}

			const task = await env.DB
				.prepare("SELECT * FROM tasks WHERE id = ?")
				.bind(taskId)
				.first<TaskRow>();

			if (!task) {
				return json({ ok: false, error: "task_not_found" }, 404);
			}
			if (!ownsTask(auth, task)) return json({ok:false,error:'forbidden'},403);

			// finish 重试时不重复发通知
			if (
				task.status === "completed" ||
				task.status === "failed" ||
				task.status === "interrupted"
			) {
				return json({
					ok: true,
					idempotent: true,
					task,
				});
			}

			const exitCode =
				typeof body.exit_code === "number"
					? body.exit_code
					: 1;

			const status = exitCode === 0 ? "completed" : "failed";

			const finishedAt = nowMs();
			const durationMs = finishedAt - task.started_at;

			const finishUpdate = await env.DB
				.prepare(`
					UPDATE tasks
					SET
						status = ?,
						finished_at = ?,
						last_heartbeat = ?,
						exit_code = ?,
						duration_ms = ?,
						metadata_json = COALESCE(?, metadata_json),
						error_summary = ?
					WHERE id = ? AND status IN ('running','lost')
				`)
				.bind(
					status,
					finishedAt,
					finishedAt,
					exitCode,
					durationMs,
					metadataToJson(body.metadata),
					body.error_summary?.slice(0, 1500) ?? null,
					taskId,
				)
				.run();
			if (!finishUpdate.meta.changes) {
				const current = await env.DB.prepare('SELECT * FROM tasks WHERE id = ?').bind(taskId).first<TaskRow>();
				return json({ok:true,idempotent:true,task:current});
			}

			await env.DB
				.prepare(`
					INSERT OR IGNORE INTO events (
						id,
						task_id,
						event_type,
						payload_json,
						created_at
					)
					VALUES (?, ?, ?, ?, ?)
				`)
				.bind(
					`task:${taskId}:finished`,
					taskId,
					status === "completed"
						? "task.completed"
						: "task.failed",
					metadataToJson(body),
					finishedAt,
				)
				.run();

			const updatedTask = await env.DB
				.prepare("SELECT * FROM tasks WHERE id = ?")
				.bind(taskId)
				.first<TaskRow>();

			return json({
				ok: true,
				task: updatedTask,
				notification_sent: false,
			});
		}

		// =====================================================
		// GET /v1/tasks/:id
		// =====================================================
		const taskMatch = url.pathname.match(/^\/v1\/tasks\/([^/]+)$/);

		if (request.method === "GET" && taskMatch) {
			const auth = await authorize(request, env, "tasks:read");
			if (!auth) {
				return json({ ok: false, error: "unauthorized" }, 401);
			}

			const taskId = taskMatch[1];

			const task = await env.DB
				.prepare("SELECT * FROM tasks WHERE id = ?")
				.bind(taskId)
				.first<TaskRow>();

			if (!task) {
				return json({ ok: false, error: "task_not_found" }, 404);
			}
			if (!ownsTask(auth, task)) return json({ok:false,error:'forbidden'},403);

			return json({
				ok: true,
				task,
			});
		}

		return json(
			{
				ok: false,
				error: "not_found",
			},
			404,
		);
	},
	async scheduled(_event, env): Promise<void> {
		const timeoutSeconds = Number(env.LOST_TIMEOUT_SECONDS ?? '420');
		const cutoff = nowMs() - (Number.isFinite(timeoutSeconds) && timeoutSeconds > 0 ? timeoutSeconds : 420) * 1000;
		const stale = await env.DB.prepare(
			"SELECT id, name, host, last_heartbeat FROM tasks WHERE status = 'running' AND last_heartbeat <= ? LIMIT 100",
		).bind(cutoff).all<Pick<TaskRow, 'id' | 'name' | 'host' | 'last_heartbeat'>>();
		for (const task of stale.results) {
			const update = await env.DB.prepare(
				"UPDATE tasks SET status = 'lost' WHERE id = ? AND status = 'running' AND last_heartbeat <= ?",
			).bind(task.id, cutoff).run();
			if (!update.meta.changes) continue;
			await env.DB.prepare(
				"INSERT OR IGNORE INTO events (id, task_id, event_type, created_at) VALUES (?, ?, 'task.lost', ?)",
			).bind(`task:${task.id}:lost:${task.last_heartbeat}`, task.id, nowMs()).run();
			try {
				await queueNotification(env,`task:${task.id}:lost:${task.last_heartbeat}`, {
					title: '🚨 Task LOST',
					message: `${task.name}\n\nHost: ${task.host ?? 'unknown'}\n超过 ${Math.floor(timeoutSeconds)} 秒没有心跳。`,
					priority: 5,
					tags: ['rotating_light'],
				});
			} catch (error) {
				console.error('Lost notification failed', task.id, error);
			}
		}
	},
} satisfies ExportedHandler<Env>;
