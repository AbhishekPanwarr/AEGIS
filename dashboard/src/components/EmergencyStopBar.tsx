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
        position: 'absolute', top: 16, right: 32, zIndex: 100,
        display: 'flex', alignItems: 'center', gap: '16px',
      }}>
        <button
          onClick={() => setOpen(true)}
          className="btn btn-danger"
          style={{ padding: '6px 16px', fontWeight: 600, fontSize: '12px', fontFamily: 'var(--font-mono)' }}
        >
          ● EMERGENCY STOP
        </button>
      </div>

      {result && (
        <div className="glass-panel" style={{ margin: '16px 32px', padding: '16px', borderColor: 'var(--halt)' }}>
          <strong style={{ color: 'var(--halt)' }}>HALT executed:</strong> epoch={result.epoch_bumped_to}, sagas_swept={result.sagas_swept}, orphaned_funds={result.orphaned_funds_minor}
        </div>
      )}

      {open && (
        <div style={{
          position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.8)', backdropFilter: 'blur(4px)',
          display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 200,
        }} onClick={() => setOpen(false)}>
          <div className="glass-panel" style={{ padding: '32px', minWidth: '400px' }} onClick={e => e.stopPropagation()}>
            <h2 className="page-title" style={{ color: 'var(--halt)', marginBottom: '24px' }}>Confirm Emergency Stop</h2>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
              <label style={{ display: 'flex', flexDirection: 'column', gap: '8px', color: 'var(--text-secondary)' }}>
                Scope Type
                <select value={scopeType} onChange={e => setScopeType(e.target.value)} style={{ padding: '8px', background: 'rgba(255,255,255,0.05)', color: '#fff', border: '1px solid rgba(255,255,255,0.1)', borderRadius: '6px' }}>
                  <option value="fleet">fleet</option>
                  <option value="group">group</option>
                  <option value="agent">agent</option>
                </select>
              </label>
              <label style={{ display: 'flex', flexDirection: 'column', gap: '8px', color: 'var(--text-secondary)' }}>
                Scope ID
                <input value={scopeId} onChange={e => setScopeId(e.target.value)} style={{ padding: '8px', background: 'rgba(255,255,255,0.05)', color: '#fff', border: '1px solid rgba(255,255,255,0.1)', borderRadius: '6px' }} />
              </label>
              <label style={{ display: 'flex', flexDirection: 'column', gap: '8px', color: 'var(--text-secondary)' }}>
                Reason
                <input value={reason} onChange={e => setReason(e.target.value)} placeholder="manual stop" style={{ padding: '8px', background: 'rgba(255,255,255,0.05)', color: '#fff', border: '1px solid rgba(255,255,255,0.1)', borderRadius: '6px' }} />
              </label>
              {error && <div style={{ color: 'var(--halt)' }}>{error}</div>}
              <div style={{ display: 'flex', gap: '12px', marginTop: '16px' }}>
                <button onClick={handleStop} className="btn btn-danger" style={{ flex: 1, padding: '12px', fontWeight: 600 }}>Confirm HALT</button>
                <button onClick={() => setOpen(false)} className="btn" style={{ flex: 1, padding: '12px', fontWeight: 600 }}>Cancel</button>
              </div>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
