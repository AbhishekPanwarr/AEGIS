import { useEffect, useState } from 'react';
import { containmentApi, type ApprovalRequest } from '../api/containment';

export function Approvals() {
  const [approvals, setApprovals] = useState<ApprovalRequest[]>([]);

  const poll = async () => {
    try {
      const aps = await containmentApi.getPendingApprovals();
      setApprovals(aps);
    } catch {}
  };

  useEffect(() => {
    poll();
    const interval = setInterval(poll, 5000);
    return () => clearInterval(interval);
  }, []);

  const handleDecide = async (id: string, decision: string) => {
    await containmentApi.decideApproval(id, decision, 'dashboard', 'manual_review');
    poll();
  };

  return (
    <div style={{ padding: '24px' }}>
      <h1 style={{ marginBottom: '16px' }}>Approvals</h1>
      <div style={{ display: 'grid', gap: '8px' }}>
        {approvals.map(a => {
          const ctx = a.decision_context || {};
          return (
            <div key={a.id} style={{
              background: '#1e293b', padding: '12px 16px', borderRadius: '8px',
            }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <strong>{ctx.action_type || 'unknown'}</strong>
                <span style={{ color: '#64748b', fontSize: '12px' }}>{a.id.slice(0, 8)}...</span>
                <span style={{ color: '#64748b', fontSize: '12px' }}>agent: {a.agent_id.slice(0, 8)}...</span>
              </div>
              <div style={{ marginTop: '4px', fontSize: '13px', color: '#94a3b8' }}>
                counterparty: {ctx.counterparty_id} | amount: {ctx.amount_minor}
              </div>
              <div style={{ marginTop: '8px', display: 'flex', gap: '8px' }}>
                <button onClick={() => handleDecide(a.id, 'APPROVED')} style={{
                  background: '#22c55e', color: '#fff', border: 'none',
                  padding: '4px 12px', borderRadius: '4px', cursor: 'pointer',
                }}>Approve</button>
                <button onClick={() => handleDecide(a.id, 'DENIED')} style={{
                  background: '#ef4444', color: '#fff', border: 'none',
                  padding: '4px 12px', borderRadius: '4px', cursor: 'pointer',
                }}>Deny</button>
              </div>
            </div>
          );
        })}
        {approvals.length === 0 && <div style={{ color: '#64748b' }}>No pending approvals.</div>}
      </div>
    </div>
  );
}
