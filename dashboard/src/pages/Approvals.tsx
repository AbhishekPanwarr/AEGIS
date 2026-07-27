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
    <div className="page-container">
      <h1 className="page-title"><span className="typing-effect">Pending_Approvals</span></h1>
      <div className="grid-list">
        {approvals.map(a => {
          const ctx = a.decision_context || {};
          return (
            <div key={a.id} className="glass-panel" style={{ padding: '16px 20px', display: 'flex', flexDirection: 'column', gap: '12px' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                <strong style={{ fontSize: '16px', color: '#fff' }}>{ctx.action_type || 'unknown'}</strong>
                <span className="agent-meta">{a.id.slice(0, 8)}...</span>
                <span className="badge badge-observe">agent: {a.agent_id.slice(0, 8)}...</span>
              </div>
              <div style={{ fontSize: '14px', color: 'var(--text-secondary)' }}>
                Counterparty: <strong style={{ color: '#fff' }}>{ctx.counterparty_id}</strong> &bull; Amount: <strong style={{ color: '#fff' }}>{ctx.amount_minor}</strong>
              </div>
              <div style={{ display: 'flex', gap: '12px', marginTop: '4px' }}>
                <button onClick={() => handleDecide(a.id, 'APPROVED')} className="btn btn-primary" style={{ flex: 1 }}>Approve</button>
                <button onClick={() => handleDecide(a.id, 'DENIED')} className="btn btn-danger" style={{ flex: 1 }}>Deny</button>
              </div>
            </div>
          );
        })}
        {approvals.length === 0 && <div className="agent-meta" style={{ textAlign: 'center', padding: '32px', background: 'rgba(255,255,255,0.02)', borderRadius: '12px' }}>No pending approvals. System running autonomously.</div>}
      </div>
    </div>
  );
}
