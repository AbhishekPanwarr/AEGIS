import { useEffect, useState } from 'react';
import { Badge } from '../components/Badge';
import { containmentApi, type ContainmentState, type ContainmentEvent } from '../api/containment';

export function Containment() {
  const [states, setStates] = useState<ContainmentState[]>([]);
  const [events, setEvents] = useState<ContainmentEvent[]>([]);
  const [form, setForm] = useState({ scopeType: 'agent', scopeId: '', level: 'THROTTLE', reason: '', actor: 'dashboard' });

  const poll = async () => {
    try {
      const [sts, evs] = await Promise.all([
        containmentApi.getStates(),
        containmentApi.getEvents(10),
      ]);
      setStates(sts);
      setEvents(evs);
    } catch {}
  };

  useEffect(() => {
    poll();
    const interval = setInterval(poll, 5000);
    return () => clearInterval(interval);
  }, []);

  const handleSetLevel = async () => {
    try {
      await containmentApi.setLevel(form.scopeType, form.scopeId, form.level, form.reason, form.actor);
      setForm({ ...form, scopeId: '', reason: '' });
      poll();
    } catch (e: any) {
      alert(e.message);
    }
  };

  return (
    <div style={{ padding: '24px' }}>
      <h1 style={{ marginBottom: '16px' }}>Containment</h1>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '24px' }}>
        <div>
          <h2 style={{ marginBottom: '12px' }}>Current States</h2>
          <div style={{ display: 'grid', gap: '6px' }}>
            {states.map(s => (
              <div key={`${s.scope_type}:${s.scope_id}`} style={{
                background: '#1e293b', padding: '8px 12px', borderRadius: '6px',
                display: 'flex', alignItems: 'center', gap: '8px',
              }}>
                <Badge level={s.level} />
                <span>{s.scope_type}:{s.scope_id.slice(0, 12)}</span>
                <span style={{ color: '#64748b', fontSize: '12px' }}>by {s.actor}</span>
              </div>
            ))}
            {states.length === 0 && <div style={{ color: '#64748b' }}>No states. Run make seed.</div>}
          </div>
        </div>

        <div>
          <h2 style={{ marginBottom: '12px' }}>Recent Events</h2>
          <div style={{ display: 'grid', gap: '4px', maxHeight: '300px', overflowY: 'auto' }}>
            {events.map(e => (
              <div key={e.id} style={{
                background: '#1e293b', padding: '6px 12px', borderRadius: '4px',
                fontSize: '12px',
              }}>
                <Badge level={e.to_level} />
                <span style={{ marginLeft: '8px' }}>{e.scope_type}:{e.scope_id.slice(0, 12)}</span>
                <span style={{ color: '#64748b' }}> by {e.actor}</span>
              </div>
            ))}
          </div>
        </div>
      </div>

      <div style={{ marginTop: '24px', background: '#1e293b', padding: '16px', borderRadius: '8px' }}>
        <h2 style={{ marginBottom: '12px' }}>Change Level</h2>
        <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap' }}>
          <select value={form.scopeType} onChange={e => setForm({ ...form, scopeType: e.target.value })}>
            <option value="fleet">fleet</option>
            <option value="group">group</option>
            <option value="agent">agent</option>
          </select>
          <input placeholder="scope ID" value={form.scopeId} onChange={e => setForm({ ...form, scopeId: e.target.value })} style={{ width: '200px' }} />
          <select value={form.level} onChange={e => setForm({ ...form, level: e.target.value })}>
            <option value="OBSERVE">OBSERVE</option>
            <option value="THROTTLE">THROTTLE</option>
            <option value="QUARANTINE">QUARANTINE</option>
            <option value="HALT">HALT</option>
          </select>
          <input placeholder="reason" value={form.reason} onChange={e => setForm({ ...form, reason: e.target.value })} style={{ width: '150px' }} />
          <button onClick={handleSetLevel} style={{
            background: '#3b82f6', color: '#fff', border: 'none',
            padding: '4px 16px', borderRadius: '4px', cursor: 'pointer',
          }}>Set Level</button>
        </div>
      </div>
    </div>
  );
}
