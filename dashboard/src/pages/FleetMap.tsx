import { useEffect, useState } from 'react';
import { Badge } from '../components/Badge';
import { identityApi, type Agent } from '../api/identity';
import { containmentApi } from '../api/containment';

export function FleetMap() {
  const [agents, setAgents] = useState<Agent[]>([]);
  const [levels, setLevels] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const poll = async () => {
      try {
        const ags = await identityApi.getAgents();
        setAgents(ags);
        const lvls: Record<string, string> = {};
        for (const a of ags) {
          try {
            const res = await containmentApi.getLevel(a.id, a.group_id, '');
            lvls[a.id] = res.level;
          } catch { lvls[a.id] = 'OBSERVE'; }
        }
        setLevels(lvls);
      } catch (e) {
        // ignore
      } finally {
        setLoading(false);
      }
    };
    poll();
    const interval = setInterval(poll, 5000);
    return () => clearInterval(interval);
  }, []);

  if (loading) return <div style={{ padding: '24px' }}>Loading...</div>;

  return (
    <div style={{ padding: '24px' }}>
      <h1 style={{ marginBottom: '16px' }}>Fleet Map</h1>
      <div style={{ display: 'grid', gap: '8px' }}>
        {agents.map(a => (
          <div key={a.id} style={{
            background: '#1e293b', padding: '12px 16px', borderRadius: '8px',
            display: 'flex', alignItems: 'center', gap: '12px',
          }}>
            <Badge level={levels[a.id] || 'OBSERVE'} />
            <span style={{ fontWeight: 600 }}>{a.persona}</span>
            <span style={{ color: '#64748b', fontSize: '12px' }}>{a.id}</span>
            <span style={{ color: '#64748b', fontSize: '12px' }}>group: {a.group_id.slice(0, 8)}...</span>
          </div>
        ))}
        {agents.length === 0 && <div style={{ color: '#64748b' }}>No agents found. Run make seed.</div>}
      </div>
    </div>
  );
}
