import { useState } from 'react';
import { containmentApi, type EmergencyStopResponse } from '../api/containment';

export function EmergencyStopBar() {
  const [open, setOpen] = useState(false);
  const [scopeType, setScopeType] = useState('fleet');
  const [scopeId, setScopeId] = useState('ALL');
  const [reason, setReason] = useState('');
  const [result, setResult] = useState<EmergencyStopResponse | null>(null);
  const [error, setError] = useState('');

  const handleStop = async () => {
    try {
      setError('');
      const res = await containmentApi.emergencyStop(scopeType, scopeId, reason || 'manual stop', 'dashboard');
      setResult(res);
      setOpen(false);
    } catch (e: any) {
      setError(e.message);
    }
  };

  return (
    <>
      <div style={{
        position: 'sticky', top: 0, zIndex: 100,
        background: '#1e293b', padding: '12px 24px',
        display: 'flex', alignItems: 'center', gap: '16px',
        borderBottom: '1px solid #334155',
      }}>
        <span style={{ fontWeight: 700, fontSize: '18px', color: '#38bdf8' }}>AEGIS</span>
        <span style={{ color: '#64748b', fontSize: '14px' }}>Control Dashboard</span>
        <div style={{ flex: 1 }} />
        <button
          onClick={() => setOpen(true)}
          style={{
            background: '#ef4444', color: '#fff', border: 'none',
            padding: '8px 20px', borderRadius: '6px', fontWeight: 700,
            cursor: 'pointer', fontSize: '14px',
          }}
        >
          EMERGENCY STOP
        </button>
      </div>

      {result && (
        <div style={{
          margin: '12px 24px', padding: '12px 16px',
          background: '#7f1d1d', borderRadius: '8px',
        }}>
          <strong>HALT executed:</strong> epoch={result.epoch_bumped_to}, sagas_swept={result.sagas_swept}, orphaned_funds={result.orphaned_funds_minor}
        </div>
      )}

      {open && (
        <div style={{
          position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.5)',
          display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 200,
        }} onClick={() => setOpen(false)}>
          <div style={{
            background: '#1e293b', padding: '24px', borderRadius: '12px',
            minWidth: '400px',
          }} onClick={e => e.stopPropagation()}>
            <h2 style={{ marginBottom: '16px' }}>Emergency Stop</h2>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
              <label>
                Scope Type:
                <select value={scopeType} onChange={e => setScopeType(e.target.value)} style={{ marginLeft: '8px' }}>
                  <option value="fleet">fleet</option>
                  <option value="group">group</option>
                  <option value="agent">agent</option>
                </select>
              </label>
              <label>
                Scope ID:
                <input value={scopeId} onChange={e => setScopeId(e.target.value)} style={{ marginLeft: '8px' }} />
              </label>
              <label>
                Reason:
                <input value={reason} onChange={e => setReason(e.target.value)} placeholder="manual stop" style={{ marginLeft: '8px' }} />
              </label>
              {error && <div style={{ color: '#ef4444' }}>{error}</div>}
              <div style={{ display: 'flex', gap: '8px', marginTop: '8px' }}>
                <button onClick={handleStop} style={{
                  background: '#ef4444', color: '#fff', border: 'none',
                  padding: '8px 16px', borderRadius: '6px', cursor: 'pointer',
                }}>Confirm HALT</button>
                <button onClick={() => setOpen(false)} style={{
                  background: '#475569', color: '#fff', border: 'none',
                  padding: '8px 16px', borderRadius: '6px', cursor: 'pointer',
                }}>Cancel</button>
              </div>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
