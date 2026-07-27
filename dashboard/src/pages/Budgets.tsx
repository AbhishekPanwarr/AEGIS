import { useEffect, useState } from 'react';
import { budgetApi, type BudgetNode, type BudgetUsage } from '../api/budget';

export function Budgets() {
  const [nodes, setNodes] = useState<BudgetNode[]>([]);
  const [usages, setUsages] = useState<Record<string, BudgetUsage>>({});
  const [editCap, setEditCap] = useState<Record<string, string>>({});

  const poll = async () => {
    try {
      const ns = await budgetApi.getNodes();
      setNodes(ns);
      const us: Record<string, BudgetUsage> = {};
      for (const n of ns) {
        try { us[n.id] = await budgetApi.getUsage(n.id); } catch {}
      }
      setUsages(us);
    } catch {}
  };

  useEffect(() => {
    poll();
    const interval = setInterval(poll, 5000);
    return () => clearInterval(interval);
  }, []);

  const handleUpdateCap = async (id: string) => {
    const cap = parseInt(editCap[id] || '0');
    if (cap > 0) {
      await budgetApi.updateCap(id, cap);
      setEditCap({ ...editCap, [id]: '' });
      poll();
    }
  };

  return (
    <div className="page-container">
      <h1 className="page-title"><span className="typing-effect">Hierarchical_Budgets</span></h1>
      <div className="grid-list">
        {nodes.map(n => {
          const u = usages[n.id];
          return (
            <div key={n.id} className="glass-panel" style={{ padding: '16px 20px', display: 'flex', flexDirection: 'column', gap: '12px' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                <strong style={{ fontSize: '18px', color: '#fff' }}>{n.label}</strong>
                <span className="agent-meta">{n.id.slice(0, 8)}...</span>
              </div>
              {u && (
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: '12px', background: 'rgba(0,0,0,0.2)', padding: '12px', borderRadius: '8px' }}>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '4px' }}><span style={{ fontSize: '11px', textTransform: 'uppercase', color: 'var(--text-secondary)' }}>Cap</span><strong style={{ color: '#fff', fontSize: '18px', fontFamily: 'var(--font-mono)' }}>${u.cap_minor.toLocaleString()}</strong></div>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '4px' }}><span style={{ fontSize: '11px', textTransform: 'uppercase', color: 'var(--text-secondary)' }}>Committed</span><strong style={{ color: '#fff', fontSize: '18px', fontFamily: 'var(--font-mono)' }}>${u.committed_minor.toLocaleString()}</strong></div>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '4px' }}><span style={{ fontSize: '11px', textTransform: 'uppercase', color: 'var(--text-secondary)' }}>Reserved</span><strong style={{ color: '#fff', fontSize: '18px', fontFamily: 'var(--font-mono)' }}>${u.reserved_minor.toLocaleString()}</strong></div>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '4px' }}><span style={{ fontSize: '11px', textTransform: 'uppercase', color: 'var(--text-secondary)' }}>Headroom</span><strong style={{ color: u.headroom_minor > 0 ? 'var(--text-accent)' : 'var(--halt)', fontSize: '18px', fontFamily: 'var(--font-mono)' }}>${u.headroom_minor.toLocaleString()}</strong></div>
                </div>
              )}
              <div style={{ display: 'flex', gap: '8px', marginTop: '4px' }}>
                <input
                  type="number"
                  placeholder="new cap"
                  value={editCap[n.id] || ''}
                  onChange={e => setEditCap({ ...editCap, [n.id]: e.target.value })}
                  style={{ width: '160px', padding: '8px 12px', background: 'rgba(255,255,255,0.05)', border: '1px solid rgba(255,255,255,0.1)', borderRadius: '6px', color: '#fff' }}
                />
                <button onClick={() => handleUpdateCap(n.id)} className="btn btn-primary">Update Cap</button>
              </div>
            </div>
          );
        })}
        {nodes.length === 0 && <div className="agent-meta" style={{ textAlign: 'center', padding: '32px' }}>No budget nodes found. Run make seed.</div>}
      </div>
    </div>
  );
}
