-- Narrow service-role-only Data API surface. Functions use invoker privileges;
-- no browser role receives access to the private nmc3 schema or these RPCs.
grant usage on schema nmc3 to service_role;
grant select, insert, update on nmc3.runs, nmc3.agent_invocations,
  nmc3.model_turns, nmc3.tool_invocations, nmc3.run_events to service_role;

create or replace function public.nmc3_create_run(p_run jsonb)
returns void language plpgsql security invoker set search_path = '' as $$
begin
  insert into nmc3.runs(id,state,agent_id,agent_version,snapshot,created_at,updated_at)
  values(p_run->>'id',p_run->>'state',p_run->>'agent_id',p_run->>'agent_version',
         p_run,(p_run->>'created_at')::timestamptz,(p_run->>'updated_at')::timestamptz);
end;
$$;

create or replace function public.nmc3_get_run(p_id text)
returns jsonb language sql security invoker set search_path = '' as $$
  select snapshot from nmc3.runs where id=p_id;
$$;

create or replace function public.nmc3_update_run(p_run jsonb)
returns void language plpgsql security invoker set search_path = '' as $$
declare
  old_state text;
  old_snapshot jsonb;
  inv jsonb;
  tool jsonb;
  tool_state text;
begin
  select state,snapshot into old_state,old_snapshot from nmc3.runs where id=p_run->>'id' for update;
  if not found then raise exception 'run not found' using errcode='P0002'; end if;
  if (old_state='PENDING' and p_run->>'state' not in ('PENDING','RUNNING','FAILED','CANCELLED'))
     or (old_state='RUNNING' and p_run->>'state' not in ('RUNNING','SUCCEEDED','FAILED','CANCELLED'))
     or (old_state in ('SUCCEEDED','FAILED','CANCELLED') and old_snapshot<>p_run)
  then raise exception 'invalid run transition' using errcode='22023'; end if;
  update nmc3.runs set state=p_run->>'state',snapshot=p_run,
    updated_at=(p_run->>'updated_at')::timestamptz where id=p_run->>'id';
  for inv in select value from pg_catalog.jsonb_array_elements(coalesce(p_run->'invocations','[]'::jsonb)) loop
    insert into nmc3.agent_invocations(id,run_id,parent_id,agent_id,agent_version,depth,state,snapshot)
    values(inv->>'id',p_run->>'id',inv->>'parent_id',inv->>'agent_id',inv->>'agent_version',
           (inv->>'depth')::integer,inv->>'state',inv)
    on conflict(id) do update set state=excluded.state,snapshot=excluded.snapshot;
  end loop;
  for tool in select value from pg_catalog.jsonb_array_elements(coalesce(p_run->'tool_invocations','[]'::jsonb)) loop
    tool_state:=case when tool->'error' is not null then 'FAILED'
                     when tool->'finished_at' is null then 'RUNNING' else 'SUCCEEDED' end;
    insert into nmc3.tool_invocations(id,invocation_id,tool_id,tool_version,state,snapshot,started_at,finished_at)
    values(tool->>'id',tool->>'agent_invocation_id',tool->>'tool_id',tool->>'tool_version',
           tool_state,tool,(tool->>'started_at')::timestamptz,(tool->>'finished_at')::timestamptz)
    on conflict(id) do update set state=excluded.state,snapshot=excluded.snapshot,
      finished_at=excluded.finished_at;
  end loop;
end;
$$;

create or replace function public.nmc3_append_event(p_event jsonb)
returns jsonb language plpgsql security invoker set search_path = '' as $$
declare
  seq bigint;
  event jsonb;
  invocation text;
begin
  update nmc3.runs set next_event_sequence=next_event_sequence+1
    where id=p_event->>'run_id' returning next_event_sequence into seq;
  if not found then raise exception 'run not found' using errcode='P0002'; end if;
  event:=pg_catalog.jsonb_set(p_event,'{sequence}',pg_catalog.to_jsonb(seq),true);
  if event->>'timestamp' is null then
    event:=pg_catalog.jsonb_set(event,'{timestamp}',pg_catalog.to_jsonb(pg_catalog.now()),true);
  end if;
  invocation:=event->>'invocation_id';
  insert into nmc3.run_events(run_id,sequence,invocation_id,event_type,occurred_at,data)
  values(event->>'run_id',seq,invocation,event->>'type',
    coalesce((event->>'timestamp')::timestamptz,pg_catalog.now()),event->'data');
  if invocation is not null and event->>'type'='MODEL_STARTED' then
    insert into nmc3.model_turns(invocation_id,ordinal,occurred_at)
    select invocation,coalesce(max(ordinal),0)+1,
      coalesce((event->>'timestamp')::timestamptz,pg_catalog.now())
    from nmc3.model_turns where invocation_id=invocation;
  elsif invocation is not null and event->>'type' in ('MODEL_SUCCEEDED','MODEL_FAILED') then
    update nmc3.model_turns set action_type=event->'data'->>'action',
      usage=event->'data'->'usage',error=case when event->'data'->>'code' is not null
      then pg_catalog.jsonb_build_object('code',event->'data'->>'code') end
    where invocation_id=invocation and ordinal=(select max(ordinal) from nmc3.model_turns
      where invocation_id=invocation);
  end if;
  return event;
end;
$$;

create or replace function public.nmc3_list_events(p_id text,p_after bigint)
returns jsonb language plpgsql security invoker set search_path = '' as $$
declare events jsonb;
begin
  if not exists(select 1 from nmc3.runs where id=p_id) then
    raise exception 'run not found' using errcode='P0002';
  end if;
  select coalesce(pg_catalog.jsonb_agg(
    pg_catalog.jsonb_build_object('sequence',sequence,'run_id',run_id,
      'invocation_id',invocation_id,'type',event_type,'timestamp',occurred_at,'data',data)
    order by sequence),'[]'::jsonb) into events
  from nmc3.run_events where run_id=p_id and sequence>p_after;
  return events;
end;
$$;

revoke all on function public.nmc3_create_run(jsonb) from public, anon, authenticated;
revoke all on function public.nmc3_get_run(text) from public, anon, authenticated;
revoke all on function public.nmc3_update_run(jsonb) from public, anon, authenticated;
revoke all on function public.nmc3_append_event(jsonb) from public, anon, authenticated;
revoke all on function public.nmc3_list_events(text,bigint) from public, anon, authenticated;
grant execute on function public.nmc3_create_run(jsonb) to service_role;
grant execute on function public.nmc3_get_run(text) to service_role;
grant execute on function public.nmc3_update_run(jsonb) to service_role;
grant execute on function public.nmc3_append_event(jsonb) to service_role;
grant execute on function public.nmc3_list_events(text,bigint) to service_role;
