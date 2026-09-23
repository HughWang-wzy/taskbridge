import { env } from 'cloudflare:test';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import worker from '../src/index';

const bindings = () => ({...env, TB_ADMIN_TOKEN:'test-admin', LOST_TIMEOUT_SECONDS:'420'});
async function request(path:string, method='GET', body?:unknown, token='test-admin') {
  return worker.fetch(new Request(`https://example.com${path}`,{method,headers:{authorization:`Bearer ${token}`,'content-type':'application/json'},body:body===undefined?undefined:JSON.stringify(body)}),bindings(),{} as ExecutionContext);
}
async function scheduled() {
  await worker.scheduled!({scheduledTime:Date.now(),cron:'*/2 * * * *'} as ScheduledEvent,bindings(),{} as ExecutionContext);
}

beforeEach(async () => {
  await env.DB.exec(`
    CREATE TABLE IF NOT EXISTS clients (id TEXT PRIMARY KEY,name TEXT NOT NULL,token_hash TEXT NOT NULL UNIQUE,scopes TEXT NOT NULL,created_at INTEGER NOT NULL,revoked_at INTEGER);
    CREATE TABLE IF NOT EXISTS tasks (id TEXT PRIMARY KEY,client_id TEXT,name TEXT NOT NULL,kind TEXT,host TEXT,platform TEXT,cwd TEXT,command TEXT,status TEXT NOT NULL,created_at INTEGER NOT NULL,started_at INTEGER NOT NULL,last_heartbeat INTEGER NOT NULL,finished_at INTEGER,exit_code INTEGER,duration_ms INTEGER,metadata_json TEXT,error_summary TEXT);
    CREATE TABLE IF NOT EXISTS events (id TEXT PRIMARY KEY,task_id TEXT,client_id TEXT,event_type TEXT NOT NULL,payload_json TEXT,created_at INTEGER NOT NULL);
    CREATE TABLE IF NOT EXISTS questions (id TEXT PRIMARY KEY,client_id TEXT,task_id TEXT,session_id TEXT,turn_id TEXT,question_type TEXT NOT NULL,question TEXT NOT NULL,options_json TEXT,status TEXT NOT NULL,answer TEXT,created_at INTEGER NOT NULL,expires_at INTEGER,answered_at INTEGER,answer_token_hash TEXT);
    CREATE TABLE IF NOT EXISTS notifications (id TEXT PRIMARY KEY,payload_json TEXT NOT NULL,created_at INTEGER NOT NULL,sent_at INTEGER,attempts INTEGER NOT NULL DEFAULT 0,next_attempt_at INTEGER NOT NULL,lease_until INTEGER NOT NULL DEFAULT 0,claim_token TEXT);
    DELETE FROM notifications; DELETE FROM questions; DELETE FROM events; DELETE FROM tasks; DELETE FROM clients;
  `);
  vi.stubGlobal('fetch',vi.fn(() => {throw new Error('Worker must not call ntfy')}));
});

describe('Watchdog and pending queue',()=>{
  it('queues one LOST alert and never calls ntfy',async()=>{
    const stale=Date.now()-421_000;
    await env.DB.prepare("INSERT INTO tasks (id,name,status,created_at,started_at,last_heartbeat) VALUES ('lost-1','Training','running',?,?,?)").bind(stale,stale,stale).run();
    await scheduled();await scheduled();
    expect((await env.DB.prepare("SELECT status FROM tasks WHERE id='lost-1'").first<{status:string}>())?.status).toBe('lost');
    expect((await env.DB.prepare("SELECT COUNT(*) AS n FROM notifications WHERE id LIKE 'task:lost-1:lost:%'").first<{n:number}>())?.n).toBe(1);
    expect(vi.mocked(fetch)).not.toHaveBeenCalled();
  });
  it('allows a relay to claim and ACK exactly once',async()=>{
    await request('/v1/notify','POST',{title:'Test',message:'Pending'});
    const claim=await request('/v1/notifications/claim','POST',{limit:1});
    expect(claim.status).toBe(200);
    const result=await claim.json() as {notifications:Array<{id:string;claim_token:string;payload:{message:string}}>};
    expect(result.notifications).toHaveLength(1);
    const item=result.notifications[0];
    expect(item.payload.message).toBe('Pending');
    expect((await (await request('/v1/notifications/claim','POST',{limit:1})).json() as {notifications:unknown[]}).notifications).toHaveLength(0);
    expect((await request(`/v1/notifications/${encodeURIComponent(item.id)}/ack`,'POST',{claim_token:item.claim_token})).status).toBe(200);
    expect((await request(`/v1/notifications/${encodeURIComponent(item.id)}/ack`,'POST',{claim_token:item.claim_token})).status).toBe(200);
    expect((await env.DB.prepare('SELECT sent_at FROM notifications WHERE id=?').bind(item.id).first<{sent_at:number|null}>())?.sent_at).toBeGreaterThan(0);
  });
  it('rejects an old claim after a new relay claims the message',async()=>{
    await request('/v1/notify','POST',{message:'Pending'});
    const first=(await (await request('/v1/notifications/claim','POST',{limit:1})).json() as {notifications:Array<{id:string;claim_token:string}>}).notifications[0];
    await env.DB.prepare('UPDATE notifications SET lease_until=0 WHERE id=?').bind(first.id).run();
    const second=(await (await request('/v1/notifications/claim','POST',{limit:1})).json() as {notifications:Array<{id:string;claim_token:string}>}).notifications[0];
    expect(second.claim_token).not.toBe(first.claim_token);
    expect((await request(`/v1/notifications/${encodeURIComponent(first.id)}/ack`,'POST',{claim_token:first.claim_token})).status).toBe(409);
    expect((await request(`/v1/notifications/${encodeURIComponent(first.id)}/ack`,'POST',{claim_token:second.claim_token})).status).toBe(200);
  });
  it('queues recovery once when a lost task heartbeats',async()=>{
    const stale=Date.now()-421_000;
    await env.DB.prepare("INSERT INTO tasks (id,name,status,created_at,started_at,last_heartbeat) VALUES ('recover-1','Training','lost',?,?,?)").bind(stale,stale,stale).run();
    expect((await (await request('/v1/tasks/recover-1/heartbeat','POST')).json() as {recovered:boolean}).recovered).toBe(true);
    await request('/v1/tasks/recover-1/heartbeat','POST');
    expect((await env.DB.prepare("SELECT COUNT(*) AS n FROM notifications WHERE id LIKE 'task:recover-1:recovered:%'").first<{n:number}>())?.n).toBe(1);
  });
});

describe('event source ownership',()=>{
  it('records finish without queueing a normal notification',async()=>{
    await request('/v1/tasks/start','POST',{id:'task-1',name:'Training'});
    const finished=await request('/v1/tasks/task-1/finish','POST',{exit_code:1});
    expect(finished.status).toBe(200);
    expect((await env.DB.prepare('SELECT COUNT(*) AS n FROM notifications').first<{n:number}>())?.n).toBe(0);
  });
  it('deduplicates Codex Stop without queueing it',async()=>{
    const body={session_id:'s1',turn_id:'t1',event:'Stop'};
    const first=await request('/v1/codex/events','POST',body);
    const second=await request('/v1/codex/events','POST',body);
    expect((await first.json() as {duplicate:boolean}).duplicate).toBe(false);
    expect((await second.json() as {duplicate:boolean}).duplicate).toBe(true);
    expect((await env.DB.prepare('SELECT COUNT(*) AS n FROM notifications').first<{n:number}>())?.n).toBe(0);
  });
  it('accepts a deterministic fallback notification only once',async()=>{
    const body={id:'task:abc:finished',payload:{title:'Done',message:'Task done',priority:3}};
    expect((await request('/v1/notifications','POST',body)).status).toBe(201);
    expect((await request('/v1/notifications','POST',body)).status).toBe(200);
    expect((await env.DB.prepare("SELECT COUNT(*) AS n FROM notifications WHERE id='task:abc:finished'").first<{n:number}>())?.n).toBe(1);
  });
});

describe('questions and auth',()=>{
  it('queues a phone question but accepts only one answer',async()=>{
    const response=await request('/v1/questions','POST',{question_type:'confirm',question:'Continue?'});
    expect(response.status).toBe(201);
    const created=await response.json() as {id:string;answer_url:string};
    expect((await env.DB.prepare('SELECT COUNT(*) AS n FROM notifications WHERE id=?').bind(`question:${created.id}:created`).first<{n:number}>())?.n).toBe(1);
	const queued=await env.DB.prepare('SELECT payload_json FROM notifications WHERE id=?').bind(`question:${created.id}:created`).first<{payload_json:string}>();
	const actions=(JSON.parse(queued!.payload_json) as {actions:Array<{action:string;clear:boolean}>}).actions;
	expect(actions).toHaveLength(2);
	expect(actions.every(action=>action.action==='http' && action.clear===true)).toBe(true);
    const url=new URL(created.answer_url);
    const answerPath=`/v1/questions/${created.id}/answer${url.search}`;
    expect((await request(answerPath,'POST',{answer:'yes'})).status).toBe(200);
	const lateDesktop=await request(`/v1/questions/${created.id}/answer-desktop`,'POST',{answer:'no'});
	expect(await lateDesktop.json()).toMatchObject({ok:true,duplicate:true,answer:'yes'});
	await env.DB.prepare('UPDATE questions SET expires_at=0 WHERE id=?').bind(created.id).run();
	const repeated=await request(answerPath,'POST',{answer:'yes'});
	expect(repeated.status).toBe(200);
	expect((await repeated.json() as {duplicate:boolean}).duplicate).toBe(true);
    expect((await (await request(`/v1/questions/${created.id}`)).json() as {question:{answer:string}}).question.answer).toBe('yes');
  });
  it('accepts a desktop answer and keeps the first answer across both devices',async()=>{
    const created=await (await request('/v1/questions','POST',{question_type:'choice',question:'Proceed?',options:['Continue','Cancel']})).json() as {id:string;answer_url:string};
    const desktopPath=`/v1/questions/${created.id}/answer-desktop`;
    const phoneURL=new URL(created.answer_url);
    const phonePath=`/v1/questions/${created.id}/answer${phoneURL.search}`;
    const desktop=await request(desktopPath,'POST',{answer:'Continue'});
    expect(desktop.status).toBe(200);
    expect(await desktop.json()).toMatchObject({ok:true,duplicate:false,answer:'Continue'});
    const phone=await request(phonePath,'POST',{answer:'Cancel'});
    expect(await phone.json()).toMatchObject({ok:true,duplicate:true});
    expect((await (await request(`/v1/questions/${created.id}`)).json() as {question:{answer:string}}).question.answer).toBe('Continue');
  });
  it('rejects a desktop answer from another client',async()=>{
    const created=await (await request('/v1/questions','POST',{question_type:'confirm',question:'Proceed?'})).json() as {id:string};
    const issued=await (await request('/v1/clients','POST',{name:'other',scopes:['questions:write']})).json() as {token:string};
    expect((await request(`/v1/questions/${created.id}/answer-desktop`,'POST',{answer:'yes'},issued.token)).status).toBe(404);
  });
  it('drops an expired question before giving it to a relay',async()=>{
    const created=await (await request('/v1/questions','POST',{question_type:'text',question:'Expired?'})).json() as {id:string};
    await env.DB.prepare('UPDATE questions SET expires_at=0 WHERE id=?').bind(created.id).run();
    const result=await (await request('/v1/notifications/claim','POST',{limit:10})).json() as {notifications:unknown[]};
    expect(result.notifications).toHaveLength(0);
  });
  it('requires a scoped client token to claim notifications',async()=>{
    const issued=await (await request('/v1/clients','POST',{name:'device',scopes:['tasks:read']})).json() as {token:string};
    expect((await request('/v1/notifications/claim','POST',{},issued.token)).status).toBe(401);
  });
});

describe('relay failure and targeted claim',()=>{
  it('releases a failed claim for retry',async()=>{
    await request('/v1/notify','POST',{message:'retry me'});
    const first=(await (await request('/v1/notifications/claim','POST',{limit:1})).json() as {notifications:Array<{id:string;claim_token:string}>}).notifications[0];
    expect((await request(`/v1/notifications/${encodeURIComponent(first.id)}/fail`,'POST',{claim_token:first.claim_token})).status).toBe(200);
    const row=await env.DB.prepare('SELECT attempts,lease_until,next_attempt_at FROM notifications WHERE id=?').bind(first.id).first<{attempts:number;lease_until:number;next_attempt_at:number}>();
    expect(row?.attempts).toBe(1);
    expect(row?.lease_until).toBe(0);
    expect(row?.next_attempt_at).toBeGreaterThan(Date.now());
  });
  it('claims a requested question before older queued messages',async()=>{
    await request('/v1/notify','POST',{message:'older'});
    const created=await (await request('/v1/questions','POST',{question_type:'confirm',question:'Approve?'})).json() as {id:string};
    const result=await (await request('/v1/notifications/claim','POST',{id:`question:${created.id}:created`})).json() as {notifications:Array<{id:string}>};
    expect(result.notifications.map(item=>item.id)).toEqual([`question:${created.id}:created`]);
  });
});
