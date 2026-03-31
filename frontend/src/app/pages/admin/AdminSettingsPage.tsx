import { useState } from 'react';
import { useApp } from '../../context/AppContext';
import { useNavigate } from 'react-router';
import {
  Bell, Shield, HelpCircle, LogOut, ChevronRight, Check, Settings,
  AlertTriangle, Activity,
} from 'lucide-react';

export default function AdminSettingsPage() {
  const { user, logout } = useApp();
  const navigate = useNavigate();
  const [emailAlerts, setEmailAlerts] = useState(true);
  const [criticalAlerts, setCriticalAlerts] = useState(true);
  const [saved, setSaved] = useState(false);
  const [name, setName] = useState(user?.name || '');
  const [email, setEmail] = useState(user?.email || '');

  const handleLogout = () => {
    logout();
    navigate('/');
  };

  const handleSave = async () => {
    await new Promise((r) => setTimeout(r, 500));
    setSaved(true);
    setTimeout(() => setSaved(false), 2500);
  };

  const initials = user?.name
    .split(' ')
    .map((n) => n[0])
    .join('')
    .toUpperCase();

  return (
    <div className="p-5 lg:p-8 max-w-2xl mx-auto">
      <div className="mb-7">
        <h1 style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, color: '#1F2421', marginBottom: '4px' }}>
          Admin settings
        </h1>
        <p style={{ color: '#4E5953', fontSize: '0.9375rem' }}>
          Manage your administrator profile and system preferences.
        </p>
      </div>

      {/* Profile */}
      <div className="bg-white rounded-xl p-5 mb-4" style={{ border: '1px solid var(--border)' }}>
        <div className="flex items-center gap-4 mb-5 pb-5" style={{ borderBottom: '1px solid var(--border)' }}>
          <div
            className="w-14 h-14 rounded-full flex items-center justify-center text-white flex-shrink-0"
            style={{ background: '#2F6B55', fontFamily: 'var(--font-heading)', fontWeight: 700, fontSize: '1.25rem' }}
          >
            {initials}
          </div>
          <div>
            <div style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '1.0625rem' }}>
              {user?.name}
            </div>
            <div style={{ color: '#4E5953', fontSize: '0.875rem' }}>
              System Administrator
            </div>
            <div style={{ color: '#2F6B55', fontSize: '0.8125rem', fontWeight: 500, marginTop: '2px' }}>
              Full access · {user?.email}
            </div>
          </div>
        </div>

        <h4 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', marginBottom: '14px', fontSize: '0.9375rem' }}>
          Profile information
        </h4>

        <div className="space-y-4">
          <div>
            <label className="block mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>
              Full name
            </label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full px-3.5 py-2.5 rounded-lg outline-none"
              style={{ border: '1.5px solid var(--border)', background: 'white', color: '#1F2421' }}
              onFocus={(e) => (e.target.style.borderColor = '#2F6B55')}
              onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
            />
          </div>
          <div>
            <label className="block mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>
              Email address
            </label>
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              className="w-full px-3.5 py-2.5 rounded-lg outline-none"
              style={{ border: '1.5px solid var(--border)', background: 'white', color: '#1F2421' }}
              onFocus={(e) => (e.target.style.borderColor = '#2F6B55')}
              onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
            />
          </div>
        </div>

        <button
          onClick={handleSave}
          className="mt-5 flex items-center gap-2 px-5 py-2.5 rounded-lg text-white text-sm transition-all"
          style={{ background: saved ? '#2E7D32' : '#2F6B55', fontWeight: 600 }}
        >
          {saved ? <><Check size={15} /> Saved</> : 'Save changes'}
        </button>
      </div>

      {/* Alert preferences */}
      <div className="bg-white rounded-xl p-5 mb-4" style={{ border: '1px solid var(--border)' }}>
        <div className="flex items-center gap-2.5 mb-4">
          <Bell size={17} color="#2F6B55" />
          <h4 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>
            Alert preferences
          </h4>
        </div>

        <div className="space-y-4">
          {[
            {
              label: 'Email operational alerts',
              desc: 'Receive digest emails for booking volume and approval rate changes.',
              value: emailAlerts,
              toggle: () => setEmailAlerts(!emailAlerts),
            },
            {
              label: 'Critical segment alerts',
              desc: 'Get notified immediately when any road segment reaches critical capacity.',
              value: criticalAlerts,
              toggle: () => setCriticalAlerts(!criticalAlerts),
            },
          ].map((item) => (
            <div key={item.label} className="flex items-center justify-between gap-3">
              <div>
                <div style={{ color: '#1F2421', fontWeight: 500, fontSize: '0.875rem' }}>{item.label}</div>
                <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>{item.desc}</div>
              </div>
              <button
                onClick={item.toggle}
                className="relative w-11 h-6 rounded-full transition-colors flex-shrink-0"
                style={{ background: item.value ? '#2F6B55' : '#D9D2C7' }}
                role="switch"
                aria-checked={item.value}
              >
                <span
                  className="absolute top-1 w-4 h-4 rounded-full bg-white transition-all shadow-sm"
                  style={{ left: item.value ? '24px' : '4px' }}
                />
              </button>
            </div>
          ))}
        </div>
      </div>

      {/* System */}
      <div className="bg-white rounded-xl overflow-hidden mb-4" style={{ border: '1px solid var(--border)' }}>
        <div className="flex items-center gap-2.5 px-5 py-4" style={{ borderBottom: '1px solid var(--border)' }}>
          <Settings size={17} color="#2F6B55" />
          <h4 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>
            System configuration
          </h4>
        </div>
        {[
          { label: 'Capacity thresholds', sub: 'Set high and critical occupancy trigger levels', icon: Activity },
          { label: 'Booking window policy', sub: 'Configure minimum advance booking time', icon: AlertTriangle },
          { label: 'Region management', sub: 'Define and configure geographic regions', icon: Settings },
        ].map((item) => (
          <button
            key={item.label}
            className="w-full flex items-center justify-between px-5 py-4 hover:bg-muted transition-colors text-left"
            style={{ borderBottom: '1px solid var(--border)' }}
          >
            <div className="flex items-center gap-3">
              <div className="w-8 h-8 rounded-lg flex items-center justify-center" style={{ background: '#F0EDE7' }}>
                <item.icon size={15} color="#4E5953" />
              </div>
              <div>
                <div style={{ color: '#1F2421', fontWeight: 500, fontSize: '0.875rem' }}>{item.label}</div>
                <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>{item.sub}</div>
              </div>
            </div>
            <ChevronRight size={16} color="#4E5953" />
          </button>
        ))}
      </div>

      {/* Security */}
      <div className="bg-white rounded-xl overflow-hidden mb-4" style={{ border: '1px solid var(--border)' }}>
        <div className="flex items-center gap-2.5 px-5 py-4" style={{ borderBottom: '1px solid var(--border)' }}>
          <Shield size={17} color="#2F6B55" />
          <h4 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>
            Security
          </h4>
        </div>
        {[
          { label: 'Change password', sub: 'Update your admin account password' },
          { label: 'Two-factor authentication', sub: 'Recommended for admin accounts' },
          { label: 'Audit log', sub: 'View all administrative actions taken' },
        ].map((item) => (
          <button
            key={item.label}
            className="w-full flex items-center justify-between px-5 py-4 hover:bg-muted transition-colors text-left"
            style={{ borderBottom: '1px solid var(--border)' }}
          >
            <div>
              <div style={{ color: '#1F2421', fontWeight: 500, fontSize: '0.875rem' }}>{item.label}</div>
              <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>{item.sub}</div>
            </div>
            <ChevronRight size={16} color="#4E5953" />
          </button>
        ))}
      </div>

      {/* Help */}
      <div className="bg-white rounded-xl overflow-hidden mb-5" style={{ border: '1px solid var(--border)' }}>
        <div className="flex items-center gap-2.5 px-5 py-4" style={{ borderBottom: '1px solid var(--border)' }}>
          <HelpCircle size={17} color="#2F6B55" />
          <h4 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>
            Help & support
          </h4>
        </div>
        {[
          { label: 'System documentation', sub: 'Technical guides for administrators' },
          { label: 'Contact engineering', sub: 'Raise issues with the development team' },
          { label: 'Terms and service level agreement', sub: 'Review the operational agreement' },
        ].map((item) => (
          <button
            key={item.label}
            className="w-full flex items-center justify-between px-5 py-4 hover:bg-muted transition-colors text-left"
            style={{ borderBottom: '1px solid var(--border)' }}
          >
            <div>
              <div style={{ color: '#1F2421', fontWeight: 500, fontSize: '0.875rem' }}>{item.label}</div>
              <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>{item.sub}</div>
            </div>
            <ChevronRight size={16} color="#4E5953" />
          </button>
        ))}
      </div>

      {/* Sign out */}
      <button
        onClick={handleLogout}
        className="w-full flex items-center justify-center gap-2 py-3 rounded-xl text-sm transition-colors"
        style={{ border: '1.5px solid #F5C2BE', color: '#B42318', background: 'white', fontWeight: 500 }}
        onMouseEnter={(e) => (e.currentTarget.style.background = '#FDECEA')}
        onMouseLeave={(e) => (e.currentTarget.style.background = 'white')}
      >
        <LogOut size={16} />
        Sign out
      </button>
    </div>
  );
}
