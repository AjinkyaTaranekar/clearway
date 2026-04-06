-- TEMPLATE ONLY - fill in a real bcrypt hash before running
-- Generate hash: htpasswd -bnBC 12 "" yourpassword | tr -d ':\n'
INSERT INTO auth.users (id, name, email, email_lower, password_hash, role, vehicle_type, license_info)
VALUES (
    'usr_admin000000000000000000000001',
    'System Admin',
    'admin@traffic.ie',
    'admin@traffic.ie',
    '$2a$12$REPLACE_THIS_WITH_REAL_BCRYPT_HASH__________________',
    'admin',
    'car',
    '{"license_number": "ADMIN001"}'
) ON CONFLICT (id) DO NOTHING;
