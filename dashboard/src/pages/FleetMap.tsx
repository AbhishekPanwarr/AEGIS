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

  if (loading) return <div className="page-container">Loading...</div>;

  return (
    <div className="page-container">
      <h1 className="page-title"><span className="typing-effect">Fleet_Map</span></h1>
      <div className="grid-list">
        {agents.map(a => (
          <div key={a.id} className="glass-panel agent-card">
            <Badge level={levels[a.id] || 'OBSERVE'} />
            <span className="agent-name">{a.persona}</span>
            <span className="agent-meta">{a.id}</span>
            <span className="agent-meta">group: {a.group_id.slice(0, 8)}...</span>
          </div>
        ))}
        {agents.length === 0 && <div style={{ color: '#64748b' }}>No agents found. Run make seed.</div>}
      </div>
    </div>
  );
}
