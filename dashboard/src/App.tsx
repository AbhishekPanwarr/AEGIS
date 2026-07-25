import { useState } from 'react';
import { EmergencyStopBar } from './components/EmergencyStopBar';
import { FleetMap } from './pages/FleetMap';
import { Budgets } from './pages/Budgets';
import { Containment } from './pages/Containment';
import { Approvals } from './pages/Approvals';

type Page = 'fleet' | 'budgets' | 'containment' | 'approvals';

export default function App() {
  const [page, setPage] = useState<Page>('fleet');

  const navBtn = (p: Page, label: string) => (
    <button
      onClick={() => setPage(p)}
      style={{
        background: page === p ? '#3b82f6' : 'transparent',
        color: '#e2e8f0', border: 'none',
        padding: '8px 16px', borderRadius: '6px', cursor: 'pointer',
        fontWeight: page === p ? 600 : 400,
      }}
    >{label}</button>
  );

  return (
    <div>
      <EmergencyStopBar />
      <div style={{ display: 'flex', gap: '4px', padding: '12px 24px' }}>
        {navBtn('fleet', 'Fleet Map')}
        {navBtn('budgets', 'Budgets')}
        {navBtn('containment', 'Containment')}
        {navBtn('approvals', 'Approvals')}
      </div>
      {page === 'fleet' && <FleetMap />}
      {page === 'budgets' && <Budgets />}
      {page === 'containment' && <Containment />}
      {page === 'approvals' && <Approvals />}
    </div>
  );
}
