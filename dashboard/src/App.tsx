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
      className={`nav-btn ${page === p ? 'active' : ''}`}
    >{label}</button>
  );

  return (
    <div style={{ padding: '40px', maxWidth: '1400px', margin: '0 auto' }}>
      <div className="app-window" style={{ minHeight: '80vh' }}>
        <EmergencyStopBar />
        <div className="nav-container">
          <div style={{ marginRight: '32px', fontFamily: 'var(--font-body)', fontWeight: 600, fontSize: '20px', letterSpacing: '-0.5px' }}>
            AEGIS<span style={{ color: 'var(--text-accent)' }}>.</span>
          </div>
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
    </div>
  );
}
