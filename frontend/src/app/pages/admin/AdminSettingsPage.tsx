import { useState } from 'react';
import { useApp } from '../../context/AppContext';
import { useNavigate } from 'react-router';
import {
  Bell, Shield, HelpCircle, LogOut, ChevronRight, Check, Settings,
  AlertTriangle, Activity,
} from 'lucide-react';

function isValidPhone(phoneNumber: string): boolean {
  if (phoneNumber === '') return true;
  if (phoneNumber.length < 7 || phoneNumber.length > 32) return false;

  let digitCount = 0;
  for (const char of phoneNumber) {
    if (char >= '0' && char <= '9') {
      digitCount += 1;
      continue;
    }
    if (char === '+' || char === '-' || char === '(' || char === ')' || char === ' ') {
      continue;
    }
    return false;
  }
  return digitCount >= 7;
}

export default function AdminSettingsPage() {
  const { user, logout, updateProfile } = useApp();
  const navigate = useNavigate();
  const [emailAlerts, setEmailAlerts] = useState(true);
  const [criticalAlerts, setCriticalAlerts] = useState(true);
  const [saved, setSaved] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [name, setName] = useState(user?.name || '');
  const [phone, setPhone] = useState(user?.phone || '');

  const handleLogout = () => {
    logout();
    navigate('/');
  };

  const handleSave = async () => {
    const trimmedName = name.trim();
    const trimmedPhone = phone.trim();

    if (!trimmedName) {
      setSaveError('Full name is required.');
      return;
    }
    if (!isValidPhone(trimmedPhone)) {
      setSaveError('Phone number must be empty or valid (7-32 chars, digits and + - ( ) space only).');
      return;
    }

    setSaving(true);
    setSaveError('');
    try {
      const fields: { name?: string; phone?: string } = {};
      if (trimmedName !== (user?.name || '')) fields.name = trimmedName;
      if (trimmedPhone !== (user?.phone || '')) fields.phone = trimmedPhone;
      if (Object.keys(fields).length > 0) {
        await updateProfile(fields);
      }
      setSaved(true);
      setTimeout(() => setSaved(false), 2500);
    } catch (err: any) {
      setSaveError(err.message ?? 'Failed to save profile. Please try again.');
    } finally {
      setSaving(false);
    }
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
              value={user?.email || ''}
              readOnly
              className="w-full px-3.5 py-2.5 rounded-lg outline-none"
              style={{ border: '1.5px solid var(--border)', background: '#F8F6F2', color: '#4E5953', cursor: 'not-allowed' }}
              title="Email address cannot be changed here"
            />
          </div>
          <div>
            <label className="block mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>
              Phone number <span style={{ color: '#4E5953', fontWeight: 400 }}>(optional)</span>
            </label>
            <input
              type="tel"
              value={phone}
              onChange={(e) => setPhone(e.target.value)}
              className="w-full px-3.5 py-2.5 rounded-lg outline-none"
              style={{ border: '1.5px solid var(--border)', background: 'white', color: '#1F2421' }}
              onFocus={(e) => (e.target.style.borderColor = '#2F6B55')}
              onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
            />
          </div>
        </div>

        {saveError && (
          <div className="mt-4 p-3 rounded-lg text-sm" style={{ background: '#FDECEA', color: '#B42318' }}>
            {saveError}
          </div>
        )}

        <button
          onClick={handleSave}
          disabled={saving}
          className="mt-5 flex items-center gap-2 px-5 py-2.5 rounded-lg text-white text-sm transition-all"
          style={{
            background: saved ? '#2E7D32' : '#2F6B55',
            fontWeight: 600,
            cursor: saving ? 'not-allowed' : 'pointer',
            opacity: saving ? 0.7 : 1,
          }}
        >
          {saved ? <><Check size={15} /> Saved</> : saving ? 'Saving...' : 'Save changes'}
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
