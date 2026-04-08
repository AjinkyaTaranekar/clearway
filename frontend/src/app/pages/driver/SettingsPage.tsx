import { AlertCircle, Bell, Check, ChevronRight, HelpCircle, LogOut, Shield } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router';
import { useApp } from '../../context/AppContext';
import { getToken } from '../../services/auth';
import {
  iamAddSecondaryVehicle,
  iamDeleteSecondaryVehicle,
  iamListVehicles,
  iamUpdatePrimaryVehicle,
  iamUpdateSecondaryVehicle,
  UserVehicle,
} from '../../services/iamApi';
import { disablePushNotifications, enablePushNotifications, isPushEnabled } from '../../services/pushNotifications';

const VEHICLE_TYPES = ['car', 'van', 'motorcycle', 'truck'] as const;

function vehicleLabel(vehicleType: string): string {
  if (vehicleType === 'truck') return 'Truck / HGV';
  return vehicleType.charAt(0).toUpperCase() + vehicleType.slice(1);
}

function normalizeLicense(licenseNumber: string): string {
  return licenseNumber.trim();
}

export default function SettingsPage() {
  const { user, logout, updateProfile } = useApp();
  const navigate = useNavigate();
  const [pushEnabled, setPushEnabled] = useState(false);
  const [emailNotifs, setEmailNotifs] = useState(true);
  const [saved, setSaved] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [pushBusy, setPushBusy] = useState(false);
  const [pushError, setPushError] = useState('');
  const [name, setName] = useState(user?.name || '');
  const [primaryVehicleType, setPrimaryVehicleType] = useState(user?.vehicle_type || 'car');
  const [primaryLicenseNumber, setPrimaryLicenseNumber] = useState('');
  const [primaryEmergency, setPrimaryEmergency] = useState(false);
  const [vehicles, setVehicles] = useState<UserVehicle[]>([]);
  const [vehiclesLoading, setVehiclesLoading] = useState(false);
  const [vehiclesBusy, setVehiclesBusy] = useState(false);
  const [secondaryVehicleType, setSecondaryVehicleType] = useState('car');
  const [secondaryLicenseNumber, setSecondaryLicenseNumber] = useState('');
  const [secondaryEmergency, setSecondaryEmergency] = useState(false);
  const [phone, setPhone] = useState(user?.phone || '');

  const secondaryVehicles = useMemo(
    () => vehicles.filter((vehicle) => !vehicle.is_primary),
    [vehicles],
  );

  const applyPrimaryVehicleState = (items: UserVehicle[]) => {
    const primary = items.find((vehicle) => vehicle.is_primary);
    if (!primary) return;

    setPrimaryVehicleType(primary.vehicle_type);
    setPrimaryLicenseNumber(primary.license_info?.license_number ?? '');
    setPrimaryEmergency(primary.is_emergency_vehicle);
  };

  const loadVehicles = async () => {
    const token = getToken();
    if (!token || !user) return;

    setVehiclesLoading(true);
    try {
      const items = await iamListVehicles(token);
      setVehicles(items);
      applyPrimaryVehicleState(items);
    } catch (err: any) {
      setSaveError(err.message ?? 'Failed to load your vehicles.');
    } finally {
      setVehiclesLoading(false);
    }
  };

  useEffect(() => {
    setPushEnabled(isPushEnabled());
  }, []);

  useEffect(() => {
    void loadVehicles();
  }, [user?.id]);

  const handleLogout = () => {
    logout();
    navigate('/');
  };

  const handleSave = async () => {
    const trimmedName = name.trim();
    const normalizedPrimaryLicense = normalizeLicense(primaryLicenseNumber);

    if (!trimmedName) {
      setSaveError('Full name is required.');
      return;
    }
    if (!normalizedPrimaryLicense) {
      setSaveError('Primary vehicle license number is required.');
      return;
    }

    const token = getToken();
    if (!token) {
      setSaveError('Session expired. Please sign in again.');
      return;
    }

    setSaving(true);
    setSaveError('');
    try {
      const fields: { name?: string; vehicle_type?: string } = {};
      if (trimmedName !== user?.name) fields.name = trimmedName;
      if (primaryVehicleType !== user?.vehicle_type) fields.vehicle_type = primaryVehicleType;

      if (Object.keys(fields).length > 0) {
        await updateProfile(fields);
      }

      await iamUpdatePrimaryVehicle(token, {
        vehicle_type: primaryVehicleType,
        license_info: {
          license_number: normalizedPrimaryLicense,
        },
        is_emergency_vehicle: primaryEmergency,
      });

      await loadVehicles();
      setSaved(true);
      setTimeout(() => setSaved(false), 2500);
    } catch (err: any) {
      setSaveError(err.message ?? 'Failed to save profile. Please try again.');
    } finally {
      setSaving(false);
    }
  };

  const handleAddSecondaryVehicle = async () => {
    const token = getToken();
    if (!token) {
      setSaveError('Session expired. Please sign in again.');
      return;
    }

    const normalizedLicense = normalizeLicense(secondaryLicenseNumber);
    if (!normalizedLicense) {
      setSaveError('Secondary vehicle license number is required.');
      return;
    }

    setVehiclesBusy(true);
    setSaveError('');
    try {
      await iamAddSecondaryVehicle(token, {
        vehicle_type: secondaryVehicleType,
        license_info: {
          license_number: normalizedLicense,
        },
        is_emergency_vehicle: secondaryEmergency,
      });
      setSecondaryLicenseNumber('');
      setSecondaryEmergency(false);
      await loadVehicles();
    } catch (err: any) {
      setSaveError(err.message ?? 'Failed to add secondary vehicle.');
    } finally {
      setVehiclesBusy(false);
    }
  };

  const handleToggleSecondaryEmergency = async (vehicle: UserVehicle) => {
    const token = getToken();
    if (!token) {
      setSaveError('Session expired. Please sign in again.');
      return;
    }

    setVehiclesBusy(true);
    setSaveError('');
    try {
      await iamUpdateSecondaryVehicle(token, vehicle.id, {
        is_emergency_vehicle: !vehicle.is_emergency_vehicle,
      });
      await loadVehicles();
    } catch (err: any) {
      setSaveError(err.message ?? 'Failed to update emergency flag.');
    } finally {
      setVehiclesBusy(false);
    }
  };

  const handleDeleteSecondaryVehicle = async (vehicle: UserVehicle) => {
    const token = getToken();
    if (!token) {
      setSaveError('Session expired. Please sign in again.');
      return;
    }

    setVehiclesBusy(true);
    setSaveError('');
    try {
      await iamDeleteSecondaryVehicle(token, vehicle.id);
      await loadVehicles();
    } catch (err: any) {
      setSaveError(err.message ?? 'Failed to delete secondary vehicle.');
    } finally {
      setVehiclesBusy(false);
    }
  };

  const handlePushToggle = async () => {
    setPushBusy(true);
    setPushError('');

    try {
      if (pushEnabled) {
        await disablePushNotifications();
        setPushEnabled(false);
        return;
      }

      await enablePushNotifications();
      setPushEnabled(true);
    } catch (err: any) {
      setPushError(err.message ?? 'Failed to enable push notifications.');
    } finally {
      setPushBusy(false);
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
              Primary vehicle type
            </label>
            <select
              value={primaryVehicleType}
              onChange={(e) => setPrimaryVehicleType(e.target.value)}
              className="w-full px-3.5 py-2.5 rounded-lg outline-none appearance-none"
              style={{ border: '1.5px solid var(--border)', background: 'white', color: '#1F2421' }}
              onFocus={(e) => (e.target.style.borderColor = '#2F6B55')}
              onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
            >
              {VEHICLE_TYPES.map((vehicleType) => (
                <option key={vehicleType} value={vehicleType}>{vehicleLabel(vehicleType)}</option>
              ))}
            </select>
          </div>
          <div>
            <label className="block mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>
              Primary vehicle license number
            </label>
            <input
              type="text"
              value={primaryLicenseNumber}
              onChange={(e) => setPrimaryLicenseNumber(e.target.value)}
              className="w-full px-3.5 py-2.5 rounded-lg outline-none"
              style={{ border: '1.5px solid var(--border)', background: 'white', color: '#1F2421' }}
              onFocus={(e) => (e.target.style.borderColor = '#2F6B55')}
              onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
            />
          </div>
          <div className="flex items-center justify-between px-3.5 py-2.5 rounded-lg" style={{ border: '1.5px solid var(--border)' }}>
            <div>
              <div style={{ color: '#1F2421', fontWeight: 500, fontSize: '0.875rem' }}>
                Emergency vehicle priority
              </div>
              <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>
                When enabled, this primary vehicle gets max booking priority.
              </div>
            </div>
            <button
              type="button"
              onClick={() => setPrimaryEmergency((prev) => !prev)}
              className="relative w-11 h-6 rounded-full transition-colors flex-shrink-0"
              style={{ background: primaryEmergency ? '#2F6B55' : '#D9D2C7' }}
              role="switch"
              aria-checked={primaryEmergency}
            >
              <span
                className="absolute top-1 w-4 h-4 rounded-full bg-white transition-all shadow-sm"
                style={{ left: primaryEmergency ? '24px' : '4px' }}
              />
            </button>
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

        <div className="mt-6 pt-5" style={{ borderTop: '1px solid var(--border)' }}>
          <h4 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', marginBottom: '14px', fontSize: '0.9375rem' }}>
            Secondary vehicles
          </h4>

          <div className="rounded-lg p-3.5 mb-4" style={{ border: '1px solid var(--border)', background: '#F8F6F2' }}>
            <div className="grid sm:grid-cols-2 gap-3">
              <div>
                <label className="block mb-1.5" style={{ color: '#1F2421', fontSize: '0.8125rem', fontWeight: 500 }}>
                  Vehicle type
                </label>
                <select
                  value={secondaryVehicleType}
                  onChange={(e) => setSecondaryVehicleType(e.target.value)}
                  className="w-full px-3 py-2 rounded-lg outline-none appearance-none"
                  style={{ border: '1.5px solid var(--border)', background: 'white', color: '#1F2421' }}
                >
                  {VEHICLE_TYPES.map((vehicleType) => (
                    <option key={vehicleType} value={vehicleType}>{vehicleLabel(vehicleType)}</option>
                  ))}
                </select>
              </div>
              <div>
                <label className="block mb-1.5" style={{ color: '#1F2421', fontSize: '0.8125rem', fontWeight: 500 }}>
                  License number
                </label>
                <input
                  type="text"
                  value={secondaryLicenseNumber}
                  onChange={(e) => setSecondaryLicenseNumber(e.target.value)}
                  className="w-full px-3 py-2 rounded-lg outline-none"
                  style={{ border: '1.5px solid var(--border)', background: 'white', color: '#1F2421' }}
                />
              </div>
            </div>

            <div className="flex items-center justify-between mt-3">
              <div>
                <div style={{ color: '#1F2421', fontSize: '0.8125rem', fontWeight: 500 }}>Emergency priority</div>
                <div style={{ color: '#4E5953', fontSize: '0.75rem' }}>Give this vehicle max booking priority.</div>
              </div>
              <button
                type="button"
                onClick={() => setSecondaryEmergency((prev) => !prev)}
                className="relative w-11 h-6 rounded-full transition-colors flex-shrink-0"
                style={{ background: secondaryEmergency ? '#2F6B55' : '#D9D2C7' }}
                role="switch"
                aria-checked={secondaryEmergency}
              >
                <span
                  className="absolute top-1 w-4 h-4 rounded-full bg-white transition-all shadow-sm"
                  style={{ left: secondaryEmergency ? '24px' : '4px' }}
                />
              </button>
            </div>

            <button
              type="button"
              onClick={handleAddSecondaryVehicle}
              disabled={vehiclesBusy}
              className="mt-3 px-4 py-2 rounded-lg text-sm text-white"
              style={{ background: '#2F6B55', fontWeight: 600, opacity: vehiclesBusy ? 0.7 : 1 }}
            >
              Add secondary vehicle
            </button>
          </div>

          {vehiclesLoading ? (
            <p style={{ color: '#4E5953', fontSize: '0.8125rem' }}>Loading your vehicles…</p>
          ) : secondaryVehicles.length === 0 ? (
            <p style={{ color: '#4E5953', fontSize: '0.8125rem' }}>
              No secondary vehicles added yet.
            </p>
          ) : (
            <div className="space-y-2.5">
              {secondaryVehicles.map((vehicle) => (
                <div key={vehicle.id} className="rounded-lg p-3" style={{ border: '1px solid var(--border)', background: 'white' }}>
                  <div className="flex items-start justify-between gap-3">
                    <div>
                      <div style={{ color: '#1F2421', fontWeight: 600, fontSize: '0.875rem' }}>
                        {vehicleLabel(vehicle.vehicle_type)}
                      </div>
                      <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>
                        License: {vehicle.license_info?.license_number || '-'}
                      </div>
                      <div style={{ color: vehicle.is_emergency_vehicle ? '#1E6639' : '#7A4500', fontSize: '0.75rem', marginTop: '3px' }}>
                        {vehicle.is_emergency_vehicle ? 'Emergency priority: enabled' : 'Emergency priority: disabled'}
                      </div>
                    </div>
                    <div className="flex gap-2">
                      <button
                        type="button"
                        onClick={() => { void handleToggleSecondaryEmergency(vehicle); }}
                        disabled={vehiclesBusy}
                        className="px-2.5 py-1.5 rounded-md text-xs"
                        style={{
                          border: '1px solid var(--border)',
                          color: '#1F2421',
                          background: vehicle.is_emergency_vehicle ? '#E8F4ED' : 'white',
                        }}
                      >
                        {vehicle.is_emergency_vehicle ? 'Unset priority' : 'Set priority'}
                      </button>
                      <button
                        type="button"
                        onClick={() => { void handleDeleteSecondaryVehicle(vehicle); }}
                        disabled={vehiclesBusy}
                        className="px-2.5 py-1.5 rounded-md text-xs"
                        style={{ border: '1px solid #F5C2BE', color: '#B42318', background: 'white' }}
                      >
                        Remove
                      </button>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
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
