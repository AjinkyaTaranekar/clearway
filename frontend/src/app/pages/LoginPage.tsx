import { useState } from 'react';
import { useNavigate } from 'react-router';
import { useApp } from '../context/AppContext';
import { registerApi } from '../services/auth';
import { Eye, EyeOff, AlertCircle } from 'lucide-react';

const features = [
  { icon: '🗺️', title: 'Pre-book your route', desc: 'Check road capacity before you drive' },
  { icon: '⚡', title: 'Instant decisions', desc: 'Get approved or rejected in seconds' },
  { icon: '📍', title: 'Live journey tracking', desc: 'Follow your journey status in real time' },
];

const VEHICLE_TYPES = [
  { value: 'car', label: 'Car' },
  { value: 'van', label: 'Van' },
  { value: 'motorcycle', label: 'Motorcycle' },
  { value: 'truck', label: 'HGV' },
];

type Tab = 'login' | 'register';

export default function LoginPage() {
  const navigate = useNavigate();
  const { login, isAuthenticated, user } = useApp();

  const [tab, setTab] = useState<Tab>('login');
  const [showPassword, setShowPassword] = useState(false);
  const [loading, setLoading] = useState(false);
  const [formError, setFormError] = useState('');

  // Login fields
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');

  // Register fields
  const [regName, setRegName] = useState('');
  const [regEmail, setRegEmail] = useState('');
  const [regPassword, setRegPassword] = useState('');
  const [regVehicle, setRegVehicle] = useState('car');
  const [regLicense, setRegLicense] = useState('');

  if (isAuthenticated && user) {
    if (user.role === 'admin') navigate('/admin');
    else navigate('/driver');
  }

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!email || !password) { setFormError('Email and password are required.'); return; }
    setFormError('');
    setLoading(true);
    try {
      await login(email, password);
      // navigate happens via isAuthenticated redirect above
    } catch (err: any) {
      setFormError(err.message ?? 'Invalid email or password.');
    } finally {
      setLoading(false);
    }
  };

  const handleRegister = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!regName || !regEmail || !regPassword || !regLicense) {
      setFormError('All fields are required.');
      return;
    }
    if (regPassword.length < 8) { setFormError('Password must be at least 8 characters.'); return; }
    setFormError('');
    setLoading(true);
    try {
      // Register creates the account, then login stores the session
      await registerApi({
        name: regName,
        email: regEmail,
        password: regPassword,
        vehicle_type: regVehicle,
        license_number: regLicense,
      });
      await login(regEmail, regPassword);
    } catch (err: any) {
      setFormError(err.message ?? 'Registration failed. Please try again.');
    } finally {
      setLoading(false);
    }
  };

  const inputStyle = (hasError = false) => ({
    border: hasError ? '1.5px solid #B42318' : '1.5px solid var(--border)',
    background: 'white',
    color: '#1F2421',
  });

  return (
    <div className="min-h-screen flex" style={{ background: '#F8F6F2' }}>
      {/* Left brand panel */}
      <div
        className="hidden lg:flex flex-col justify-between w-[480px] flex-shrink-0 p-10"
        style={{ background: '#1F2421', color: 'white' }}
      >
        <div>
          <div className="flex items-center gap-3 mb-16">
            <div className="w-10 h-10 rounded-xl flex items-center justify-center" style={{ background: '#2F6B55' }}>
              <svg width="20" height="20" viewBox="0 0 16 16" fill="none">
                <path d="M8 1.5L14 8L8 14.5L2 8L8 1.5Z" fill="white" opacity="0.25" />
                <path d="M8 4L11.5 8L8 12L4.5 8L8 4Z" fill="white" opacity="0.55" />
                <circle cx="8" cy="8" r="2.5" fill="white" />
              </svg>
            </div>
            <div>
              <div style={{ fontFamily: 'var(--font-heading)', fontWeight: 800, fontSize: '1.25rem', letterSpacing: '-0.01em' }}>
                Clearway
              </div>
              <div style={{ fontSize: '0.7rem', color: 'rgba(255,255,255,0.45)', letterSpacing: '0.05em', textTransform: 'uppercase', fontWeight: 500 }}>
                Road Journey Platform
              </div>
            </div>
          </div>

          <div className="mb-10">
            <h2 style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, fontSize: '2rem', lineHeight: 1.2, marginBottom: '1rem', color: 'white' }}>
              Drive ahead,<br />always in the clear.
            </h2>
            <p style={{ color: 'rgba(255,255,255,0.55)', fontSize: '1rem', lineHeight: 1.65 }}>
              Pre-book your road journey and know before you go. Clearway checks every segment of your route so you drive with confidence.
            </p>
          </div>

          <div className="space-y-5">
            {features.map((f) => (
              <div key={f.title} className="flex items-start gap-4">
                <div className="w-10 h-10 rounded-lg flex items-center justify-center flex-shrink-0 text-lg" style={{ background: 'rgba(255,255,255,0.06)' }}>
                  {f.icon}
                </div>
                <div>
                  <div style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, fontSize: '0.9375rem', color: 'white', marginBottom: '2px' }}>
                    {f.title}
                  </div>
                  <div style={{ color: 'rgba(255,255,255,0.45)', fontSize: '0.875rem' }}>{f.desc}</div>
                </div>
              </div>
            ))}
          </div>
        </div>

        <div style={{ color: 'rgba(255,255,255,0.2)', fontSize: '0.8rem' }}>
          © 2026 Clearway. Distributed Traffic Service.
        </div>
      </div>

      {/* Right auth panel */}
      <div className="flex-1 flex items-center justify-center p-6 lg:p-12">
        <div className="w-full max-w-md">
          {/* Mobile logo */}
          <div className="flex items-center gap-2.5 mb-8 lg:hidden">
            <div className="w-9 h-9 rounded-xl flex items-center justify-center" style={{ background: '#2F6B55' }}>
              <svg width="18" height="18" viewBox="0 0 16 16" fill="none">
                <circle cx="8" cy="8" r="2.5" fill="white" />
                <path d="M8 4L11.5 8L8 12L4.5 8L8 4Z" fill="white" opacity="0.5" />
              </svg>
            </div>
            <span style={{ fontFamily: 'var(--font-heading)', fontWeight: 800, fontSize: '1.25rem', color: '#1F2421' }}>Clearway</span>
          </div>

          <div className="bg-white rounded-2xl p-8 shadow-sm" style={{ border: '1px solid var(--border)' }}>
            {/* Tab switcher */}
            <div className="flex rounded-lg p-1 mb-6" style={{ background: '#F0EDE7' }}>
              {(['login', 'register'] as Tab[]).map((t) => (
                <button
                  key={t}
                  onClick={() => { setTab(t); setFormError(''); }}
                  className="flex-1 py-2 rounded-md text-sm transition-all duration-150"
                  style={{
                    background: tab === t ? 'white' : 'transparent',
                    color: tab === t ? '#1F2421' : '#4E5953',
                    fontWeight: tab === t ? 600 : 400,
                    boxShadow: tab === t ? '0 1px 3px rgba(0,0,0,0.08)' : 'none',
                    fontFamily: 'var(--font-body)',
                  }}
                >
                  {t === 'login' ? 'Sign in' : 'Create account'}
                </button>
              ))}
            </div>

            {formError && (
              <div
                className="flex items-center gap-2 p-3 rounded-lg mb-4"
                style={{ background: 'var(--status-rejected-bg)', color: 'var(--status-rejected-text)' }}
                role="alert"
              >
                <AlertCircle size={16} />
                <span className="text-sm">{formError}</span>
              </div>
            )}

            {tab === 'login' ? (
              <form onSubmit={handleLogin} noValidate>
                <div className="mb-4">
                  <div className="mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>Email address</div>
                  <input
                    type="email"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    placeholder="you@example.com"
                    className="w-full px-3.5 py-2.5 rounded-lg outline-none transition-all"
                    style={inputStyle()}
                    onFocus={(e) => (e.target.style.borderColor = '#2F6B55')}
                    onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
                  />
                </div>

                <div className="mb-6">
                  <div className="flex items-center justify-between mb-1.5">
                    <span style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>Password</span>
                  </div>
                  <div className="relative">
                    <input
                      type={showPassword ? 'text' : 'password'}
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                      placeholder="••••••••"
                      className="w-full px-3.5 py-2.5 pr-10 rounded-lg outline-none transition-all"
                      style={inputStyle()}
                      onFocus={(e) => (e.target.style.borderColor = '#2F6B55')}
                      onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
                    />
                    <button
                      type="button"
                      onClick={() => setShowPassword(!showPassword)}
                      className="absolute right-3 top-1/2 -translate-y-1/2"
                      aria-label={showPassword ? 'Hide password' : 'Show password'}
                    >
                      {showPassword ? <EyeOff size={16} color="#4E5953" /> : <Eye size={16} color="#4E5953" />}
                    </button>
                  </div>
                </div>

                <button
                  type="submit"
                  disabled={loading}
                  className="w-full py-3 rounded-lg transition-all duration-150 flex items-center justify-center gap-2"
                  style={{
                    background: loading ? '#4E5953' : '#2F6B55',
                    color: 'white',
                    fontWeight: 600,
                    fontSize: '0.9375rem',
                    cursor: loading ? 'not-allowed' : 'pointer',
                  }}
                >
                  {loading ? (
                    <><span className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin" />Signing in…</>
                  ) : 'Sign in'}
                </button>
              </form>
            ) : (
              <form onSubmit={handleRegister} noValidate>
                <div className="space-y-4 mb-6">
                  <div>
                    <div className="mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>Full name</div>
                    <input
                      type="text"
                      value={regName}
                      onChange={(e) => setRegName(e.target.value)}
                      placeholder="Alex Chen"
                      className="w-full px-3.5 py-2.5 rounded-lg outline-none transition-all"
                      style={inputStyle()}
                      onFocus={(e) => (e.target.style.borderColor = '#2F6B55')}
                      onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
                    />
                  </div>

                  <div>
                    <div className="mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>Email address</div>
                    <input
                      type="email"
                      value={regEmail}
                      onChange={(e) => setRegEmail(e.target.value)}
                      placeholder="you@example.com"
                      className="w-full px-3.5 py-2.5 rounded-lg outline-none transition-all"
                      style={inputStyle()}
                      onFocus={(e) => (e.target.style.borderColor = '#2F6B55')}
                      onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
                    />
                  </div>

                  <div>
                    <div className="mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>Password</div>
                    <div className="relative">
                      <input
                        type={showPassword ? 'text' : 'password'}
                        value={regPassword}
                        onChange={(e) => setRegPassword(e.target.value)}
                        placeholder="Min 8 characters"
                        className="w-full px-3.5 py-2.5 pr-10 rounded-lg outline-none transition-all"
                        style={inputStyle()}
                        onFocus={(e) => (e.target.style.borderColor = '#2F6B55')}
                        onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
                      />
                      <button type="button" onClick={() => setShowPassword(!showPassword)} className="absolute right-3 top-1/2 -translate-y-1/2">
                        {showPassword ? <EyeOff size={16} color="#4E5953" /> : <Eye size={16} color="#4E5953" />}
                      </button>
                    </div>
                  </div>

                  <div>
                    <div className="mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>Vehicle type</div>
                    <select
                      value={regVehicle}
                      onChange={(e) => setRegVehicle(e.target.value)}
                      className="w-full px-3.5 py-2.5 rounded-lg outline-none appearance-none"
                      style={inputStyle()}
                    >
                      {VEHICLE_TYPES.map((v) => <option key={v.value} value={v.value}>{v.label}</option>)}
                    </select>
                  </div>

                  <div>
                    <div className="mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>Licence number</div>
                    <input
                      type="text"
                      value={regLicense}
                      onChange={(e) => setRegLicense(e.target.value)}
                      placeholder="DL-123456"
                      className="w-full px-3.5 py-2.5 rounded-lg outline-none transition-all"
                      style={inputStyle()}
                      onFocus={(e) => (e.target.style.borderColor = '#2F6B55')}
                      onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
                    />
                  </div>
                </div>

                <button
                  type="submit"
                  disabled={loading}
                  className="w-full py-3 rounded-lg transition-all duration-150 flex items-center justify-center gap-2"
                  style={{
                    background: loading ? '#4E5953' : '#2F6B55',
                    color: 'white',
                    fontWeight: 600,
                    fontSize: '0.9375rem',
                    cursor: loading ? 'not-allowed' : 'pointer',
                  }}
                >
                  {loading ? (
                    <><span className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin" />Creating account…</>
                  ) : 'Create account'}
                </button>
              </form>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
