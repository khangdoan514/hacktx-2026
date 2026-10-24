-- Event provenance for the OpenCode backend. RunEvent.source marks whether
-- an event was recorded by the NMC3 coordinator ("nmc3") or mapped from an
-- OpenCode session ("opencode"). Older rows keep a NULL source, which reads
-- as absent; no backfill is needed.

alter table nmc3.run_events add column if not exists source text;

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
  insert into nmc3.run_events(run_id,sequence,invocation_id,event_type,occurred_at,data,source)
  values(event->>'run_id',seq,invocation,event->>'type',
    coalesce((event->>'timestamp')::timestamptz,pg_catalog.now()),event->'data',event->>'source');
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
      'invocation_id',invocation_id,'type',event_type,'timestamp',occurred_at,'data',data,
      'source',source)
    order by sequence),'[]'::jsonb) into events
  from nmc3.run_events where run_id=p_id and sequence>p_after;
  return events;
end;
$$;

revoke all on function public.nmc3_append_event(jsonb) from public, anon, authenticated;
revoke all on function public.nmc3_list_events(text,bigint) from public, anon, authenticated;
grant execute on function public.nmc3_append_event(jsonb) to service_role;
grant execute on function public.nmc3_list_events(text,bigint) to service_role;
