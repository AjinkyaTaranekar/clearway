import { AlertCircle, Bell, Check, ChevronRight, HelpCircle, LogOut, Shield } from 'lucide-react';
import { useState } from 'react';
import { useNavigate } from 'react-router';
import { useApp } from '../../context/AppContext';

export default function SettingsPage() {
  const { user, logout, updateProfile } = useApp();
  const navigate = useNavigate();
  const [pushEnabled, setPushEnabled] = useState(false);
  const [emailNotifs, setEmailNotifs] = useState(true);
  const [saved, setSaved] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [name, setName] = useState(user?.name || '');
  const [vehicleType, setVehicleType] = useState(user?.vehicle_type || '');
  const [phone, setPhone] = useState(user?.phone || '');

  const handleLogout = () => {
    logout();
    navigate('/');
  };

  const handleSave = async () => {
    if (!name.trim()) {
      setSaveError('Full name is required.');
      return;
    }
    setSaving(true);
    setSaveError('');
    try {
      const fields: { name?: string; vehicle_type?: string } = { name: name.trim() };
      if (vehicleType && vehicleType !== user?.vehicle_type) fields.vehicle_type = vehicleType;
      await updateProfile(fields);
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
          Settings
        </h1>
        <p style={{ color: '#4E5953', fontSize: '0.9375rem' }}>
          Manage your profile and preferences.
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
              {user?.role === 'admin' ? 'System Administrator' : 'Driver'} · {user?.email}
            </div>
            {user?.vehicleId && (
              <div style={{ color: '#2F6B55', fontSize: '0.8125rem', fontWeight: 500, marginTop: '2px' }}>
                Vehicle: {user.vehicleId}
              </div>
            )}
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
              Vehicle type
            </label>
            <select
              value={vehicleType}
              onChange={(e) => setVehicleType(e.target.value)}
              className="w-full px-3.5 py-2.5 rounded-lg outline-none appearance-none"
              style={{ border: '1.5px solid var(--border)', background: 'white', color: '#1F2421' }}
              onFocus={(e) => (e.target.style.borderColor = '#2F6B55')}
              onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
            >
              {['car', 'van', 'motorcycle', 'truck'].map((v) => (
                <option key={v} value={v}>{v.charAt(0).toUpperCase() + v.slice(1)}</option>
              ))}
            </select>
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
            <p className="mt-1" style={{ color: '#4E5953', fontSize: '0.8125rem' }}>
              Contact support to change your email address.
            </p>
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
          <div
            className="flex items-center gap-2 mt-4 p-3 rounded-lg text-sm"
            style={{ background: 'var(--status-rejected-bg)', color: 'var(--status-rejected-text)' }}
          >
            <AlertCircle size={15} />
            {saveError}
          </div>
        )}

        <button
          onClick={handleSave}
          disabled={saving}
          className="mt-5 flex items-center gap-2 px-5 py-2.5 rounded-lg text-white text-sm transition-all"
          style={{ background: saved ? '#2E7D32' : '#2F6B55', fontWeight: 600, cursor: saving ? 'not-allowed' : 'pointer', opacity: saving ? 0.7 : 1 }}
          onMouseEnter={(e) => !saved && !saving && (e.currentTarget.style.background = '#245343')}
          onMouseLeave={(e) => !saved && !saving && (e.currentTarget.style.background = '#2F6B55')}
        >
          {saved ? (
            <><Check size={15} /> Saved</>
          ) : saving ? (
            <><span className="w-3.5 h-3.5 border-2 border-white/30 border-t-white rounded-full animate-spin" /> Saving…</>
          ) : (
            'Save changes'
          )}
        </button>
      </div>

      {/* Notifications */}
      <div className="bg-white rounded-xl p-5 mb-4" style={{ border: '1px solid var(--border)' }}>
        <div className="flex items-center gap-2.5 mb-4">
          <Bell size={17} color="#2F6B55" />
          <h4 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>
            Notifications
          </h4>
        </div>

        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <div style={{ color: '#1F2421', fontWeight: 500, fontSize: '0.875rem' }}>Push notifications</div>
              <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>
                {pushEnabled ? 'Enabled - you\'ll receive alerts on this device.' : 'Off - turn on to get journey alerts instantly.'}
              </div>
            </div>
            <button
              onClick={() => setPushEnabled(!pushEnabled)}
              className="relative w-11 h-6 rounded-full transition-colors flex-shrink-0"
              style={{ background: pushEnabled ? '#2F6B55' : '#D9D2C7' }}
              role="switch"
              aria-checked={pushEnabled}
            >
              <span
                className="absolute top-1 w-4 h-4 rounded-full bg-white transition-all shadow-sm"
                style={{ left: pushEnabled ? '24px' : '4px' }}
              />
            </button>
          </div>

          <div className="flex items-center justify-between">
            <div>
              <div style={{ color: '#1F2421', fontWeight: 500, fontSize: '0.875rem' }}>Email notifications</div>
              <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>
                Receive journey updates by email as backup.
              </div>
            </div>
            <button
              onClick={() => setEmailNotifs(!emailNotifs)}
              className="relative w-11 h-6 rounded-full transition-colors flex-shrink-0"
              style={{ background: emailNotifs ? '#2F6B55' : '#D9D2C7' }}
              role="switch"
              aria-checked={emailNotifs}
            >
              <span
                className="absolute top-1 w-4 h-4 rounded-full bg-white transition-all shadow-sm"
                style={{ left: emailNotifs ? '24px' : '4px' }}
              />
            </button>
          </div>
        </div>

        {!pushEnabled && (
          <div
            className="mt-4 p-3 rounded-lg text-sm"
            style={{ background: '#FFF4E0', color: '#7A4500' }}
          >
            Push is off. Your in-app notification centre will still show all updates so you never miss anything.
          </div>
        )}
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
          { label: 'Change password', sub: 'Update your account password' },
          { label: 'Two-factor authentication', sub: 'Add an extra layer of security' },
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
          { label: 'How journeys are assessed', sub: 'Learn about road capacity checks' },
          { label: 'Contact support', sub: 'Get help from our team' },
          { label: 'Terms and conditions', sub: 'Read the service agreement' },
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
