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
    <div style={{ padding: '24px' }}>
      <h1 style={{ marginBottom: '16px' }}>Budgets</h1>
      <div style={{ display: 'grid', gap: '8px' }}>
        {nodes.map(n => {
          const u = usages[n.id];
          return (
            <div key={n.id} style={{
              background: '#1e293b', padding: '12px 16px', borderRadius: '8px',
            }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                <strong>{n.label}</strong>
                <span style={{ color: '#64748b', fontSize: '12px' }}>{n.id.slice(0, 8)}...</span>
              </div>
              {u && (
                <div style={{ marginTop: '8px', fontSize: '13px', color: '#94a3b8' }}>
                  cap: {u.cap_minor.toLocaleString()} |
                  committed: {u.committed_minor.toLocaleString()} |
                  reserved: {u.reserved_minor.toLocaleString()} |
                  headroom: <span style={{ color: u.headroom_minor > 0 ? '#22c55e' : '#ef4444' }}>{u.headroom_minor.toLocaleString()}</span>
                </div>
              )}
              <div style={{ marginTop: '8px', display: 'flex', gap: '8px' }}>
                <input
                  type="number"
                  placeholder="new cap"
                  value={editCap[n.id] || ''}
                  onChange={e => setEditCap({ ...editCap, [n.id]: e.target.value })}
                  style={{ width: '120px', padding: '4px 8px', background: '#0f172a', border: '1px solid #334155', borderRadius: '4px', color: '#e2e8f0' }}
                />
                <button onClick={() => handleUpdateCap(n.id)} style={{
                  background: '#3b82f6', color: '#fff', border: 'none',
                  padding: '4px 12px', borderRadius: '4px', cursor: 'pointer',
                }}>Update</button>
              </div>
            </div>
          );
        })}
        {nodes.length === 0 && <div style={{ color: '#64748b' }}>No budget nodes found. Run make seed.</div>}
      </div>
    </div>
  );
}
