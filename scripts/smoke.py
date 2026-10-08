#!/usr/bin/env python3
"""API integration test against a running stack; uses only generated test users."""
import argparse, json, os, pathlib, secrets, time, urllib.error, urllib.request

parser=argparse.ArgumentParser()
parser.add_argument('--url',default='http://127.0.0.1:8080')
parser.add_argument('--fixture',default='.qa/restart-fixture.json')
parser.add_argument('--verify-restart',action='store_true')
args=parser.parse_args()
checks=[]

def call(method,path,data=None,token=None,expected=200):
    raw=None if data is None else json.dumps(data).encode()
    headers={'Content-Type':'application/json'}
    if token:headers['Authorization']='Bearer '+token
    request=urllib.request.Request(args.url.rstrip('/')+path,data=raw,headers=headers,method=method)
    try:
        with urllib.request.urlopen(request,timeout=10) as response:
            status=response.status;body=response.read()
    except urllib.error.HTTPError as error:
        status=error.code;body=error.read()
    assert status==expected,(method,path,status,body[:150] if status!=201 else 'unexpected creation')
    return json.loads(body) if body and (body.startswith(b'{') or body.startswith(b'[')) else body.decode()

fixture=pathlib.Path(args.fixture)
if args.verify_restart:
    state=json.loads(fixture.read_text())
    events=call('GET','/api/events',token=state['token'])
    assert any(e['id']==state['eventId'] for e in events),'event lost after restart'
    call('DELETE','/api/events/'+state['eventId'],token=state['token'],expected=204)
    call('POST','/api/auth/logout',token=state['token'],expected=204)
    call('GET','/api/events',token=state['token'],expected=401)
    fixture.unlink()
    print(json.dumps({'restartPersistence':True,'sessionRevocation':True,'cleanup':True}))
    raise SystemExit(0)

assert call('GET','/test')=='Hello!'
checks.append('test endpoint')
call('GET','/api/events',expected=401)
suffix=secrets.token_hex(8);password=secrets.token_hex(16)+'Test!'
first=call('POST','/api/auth/register',{'name':'Integration One','email':suffix+'-one@example.invalid','password':password},expected=201)
second=call('POST','/api/auth/register',{'name':'Integration Two','email':suffix+'-two@example.invalid','password':password},expected=201)
call('POST','/api/auth/login',{'email':suffix+'-one@example.invalid','password':'IncorrectPassword123!'},expected=401)
checks.append('registration and wrong-password rejection')
payload={'title':'Integration owned event','date':time.strftime('%Y-%m-%d'),'start':'10:00','end':'11:00','category':'work','allDay':False,'description':'API test','location':'','completed':False}
event=call('POST','/api/events',payload,first['token'],201)
assert event['processingStatus']=='pending'
assert call('GET','/api/events',token=second['token'])==[]
call('PUT','/api/events/'+event['id'],payload,second['token'],404)
call('DELETE','/api/events/'+event['id'],token=second['token'],expected=404)
assert 'Integration owned event' not in call('GET','/api/export',token=second['token'])
checks.append('owner isolation for list, edit, delete and export')
deadline=time.monotonic()+20
while time.monotonic()<deadline:
    result=call('GET','/api/events',token=first['token'])
    if next(e for e in result if e['id']==event['id'])['processingStatus']=='ready':break
    time.sleep(.2)
else:raise AssertionError('worker did not return ready status')
checks.append('RabbitMQ worker round trip')
payload['title']='Integration updated'
updated=call('PUT','/api/events/'+event['id'],payload,first['token'])
assert updated['processingStatus']=='pending'
assert 'SUMMARY:Integration updated' in call('GET','/api/export',token=first['token'])
checks.append('update and iCalendar')
raw=urllib.request.Request(args.url+'/dbtest',data=b'integration database write',headers={'Authorization':'Bearer '+first['token']},method='POST')
with urllib.request.urlopen(raw,timeout=10) as response:assert response.status==201
checks.append('dbtest write')
fixture.parent.mkdir(exist_ok=True)
with os.fdopen(os.open(fixture,os.O_WRONLY|os.O_CREAT|os.O_TRUNC,0o600),'w') as output:
    json.dump({'token':first['token'],'eventId':event['id']},output)
print(json.dumps({'passed':checks,'restartFixtureSaved':True},ensure_ascii=False))
