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
    <div className="page-container">
      <h1 className="page-title"><span className="typing-effect">Containment_Protocol</span></h1>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '24px' }}>
        <div>
          <h2 style={{ marginBottom: '16px', color: 'var(--text-secondary)' }}>Current States</h2>
          <div className="grid-list">
            {states.map(s => (
              <div key={`${s.scope_type}:${s.scope_id}`} className="glass-panel agent-card">
                <Badge level={s.level} />
                <span className="agent-name">{s.scope_type}:{s.scope_id.slice(0, 12)}</span>
                <span className="agent-meta">by {s.actor}</span>
              </div>
            ))}
            {states.length === 0 && <div className="agent-meta">No states. Run make seed.</div>}
          </div>
        </div>

        <div>
          <h2 style={{ marginBottom: '16px', color: 'var(--text-secondary)' }}>Recent Events</h2>
          <div className="grid-list" style={{ maxHeight: '400px', overflowY: 'auto' }}>
            {events.map(e => (
              <div key={e.id} className="glass-panel" style={{ padding: '12px 16px', display: 'flex', alignItems: 'center', gap: '12px' }}>
                <Badge level={e.to_level} />
                <span className="agent-name">{e.scope_type}:{e.scope_id.slice(0, 12)}</span>
                <span className="agent-meta">by {e.actor}</span>
              </div>
            ))}
          </div>
        </div>
      </div>

      <div className="glass-panel" style={{ marginTop: '32px', padding: '24px' }}>
        <h2 style={{ marginBottom: '16px', color: 'var(--text-secondary)' }}>Change Level</h2>
        <div style={{ display: 'flex', gap: '12px', flexWrap: 'wrap' }}>
          <select value={form.scopeType} onChange={e => setForm({ ...form, scopeType: e.target.value })} style={{ padding: '8px 12px', background: 'rgba(255,255,255,0.05)', color: '#fff', border: '1px solid rgba(255,255,255,0.1)', borderRadius: '6px' }}>
            <option value="fleet">fleet</option>
            <option value="group">group</option>
            <option value="agent">agent</option>
          </select>
          <input placeholder="scope ID" value={form.scopeId} onChange={e => setForm({ ...form, scopeId: e.target.value })} style={{ width: '250px', padding: '8px 12px', background: 'rgba(255,255,255,0.05)', color: '#fff', border: '1px solid rgba(255,255,255,0.1)', borderRadius: '6px' }} />
          <select value={form.level} onChange={e => setForm({ ...form, level: e.target.value })} style={{ padding: '8px 12px', background: 'rgba(255,255,255,0.05)', color: '#fff', border: '1px solid rgba(255,255,255,0.1)', borderRadius: '6px' }}>
            <option value="OBSERVE">OBSERVE</option>
            <option value="THROTTLE">THROTTLE</option>
            <option value="QUARANTINE">QUARANTINE</option>
            <option value="HALT">HALT</option>
          </select>
          <input placeholder="reason" value={form.reason} onChange={e => setForm({ ...form, reason: e.target.value })} style={{ width: '200px', padding: '8px 12px', background: 'rgba(255,255,255,0.05)', color: '#fff', border: '1px solid rgba(255,255,255,0.1)', borderRadius: '6px' }} />
          <button onClick={handleSetLevel} className="btn btn-primary">Set Level</button>
        </div>
      </div>
    </div>
  );
}
