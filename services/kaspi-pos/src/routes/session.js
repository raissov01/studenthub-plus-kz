import { Router } from 'express';
import { KASPI_QRPAY_URL } from '../config.js';
import { loggedFetch, signedQrPayHeaders } from '../helpers.js';
import { decryptSecret } from '../crypto.js';
import { fetchOrgContext } from './auth.js';

const router = Router();

// Extract session from request headers
const extractSession = (req) => ({
  tokenSN: req.headers['x-token-sn'] || null,
  profileId: req.headers['x-profile-id'] || null,
  vtokenSecret: req.headers['x-vtoken-secret'] || null,
});

// ─── Check session validity ───

router.get('/check', async (req, res) => {
  const session = extractSession(req);

  // 1. Check required headers
  if (!session.tokenSN) return res.status(401).json({ active: false, error: 'Missing X-Token-SN header.' });
  if (!session.vtokenSecret) return res.status(401).json({ active: false, error: 'Missing X-Vtoken-Secret header.' });

  // 2. Try to decrypt vtokenSecret
  try {
    session.decryptedSecret = decryptSecret(session.vtokenSecret);
  } catch {
    return res.status(401).json({ active: false, error: 'Invalid or expired vtokenSecret. Re-authenticate.' });
  }

  // 3. Ping Kaspi API to verify the token is still accepted
  try {
    const url = `${KASPI_QRPAY_URL}/v02/history/operations`;
    const payload = JSON.stringify({
      EndDate: new Date().toISOString().slice(0, 10),
      LastTransactionDate: '',
      StatementPeriodCode: 0,
    });
    const headers = { ...signedQrPayHeaders(url, session, payload), 'Content-Type': 'application/json' };
    const resp = await loggedFetch(url, {
      method: 'POST',
      headers,
      body: payload,
    });

    const body = await resp.json().catch(() => ({}));

    // Kaspi may return HTTP 200 but with error StatusCode in body
    if (resp.ok && (!body.StatusCode || body.StatusCode === 0)) {
      return res.json({ active: true });
    }

    return res.status(resp.ok ? 401 : resp.status).json({
      active: false,
      error: body.Message || body.message || 'Session rejected by Kaspi API.',
      code: body.StatusCode || body.Code,
      details: body,
    });
  } catch (err) {
    return res.status(500).json({ active: false, error: err.message });
  }
});

// ─── Switch business (Student Hub patch, experimental) ───
// A cashier number added to several businesses has ONE session; each business is a different profileId
// (X-PI). Returns the profile of `organizationId` so the caller can store it per merchant. Nothing is
// stored here. Kaspi's behaviour for this call is not documented; verify with a test payment.

router.post('/org-context', async (req, res) => {
  const session = extractSession(req);
  if (!session.tokenSN || !session.vtokenSecret) return res.status(401).json({ error: 'Missing session headers.' });
  const raw = req.body?.organizationId;
  const organizationId = typeof raw === 'number' || (typeof raw === 'string' && /^\d+$/.test(raw)) ? Number(raw) : NaN;
  if (!Number.isSafeInteger(organizationId) || organizationId < 0) {
    return res.status(400).json({ error: 'organizationId must be a non-negative integer' });
  }
  let rawSecret;
  try {
    rawSecret = decryptSecret(session.vtokenSecret);
  } catch {
    return res.status(401).json({ active: false, error: 'Invalid or expired vtokenSecret. Re-authenticate.' });
  }
  try {
    const body = await fetchOrgContext(session.tokenSN, rawSecret, organizationId);
    const cur = body?.Data?.Current;
    if (!cur?.ProfileId) {
      return res.status(502).json({ error: body?.Message || 'Kaspi returned no profile for this organization.', code: body?.StatusCode });
    }
    const organizations = Array.isArray(body.Data.Organizations)
      ? body.Data.Organizations.map((o) => ({ organizationId: o.OrganizationId ?? o.Id ?? null, name: o.OrganizationName ?? o.Name ?? null }))
      : [];
    return res.json({
      profileId: cur.ProfileId,
      organizationId: cur.OrganizationId ?? organizationId,
      orgName: cur.OrganizationName ?? null,
      isCashier: cur.IsCashier ?? null,
      accessLevelType: cur.AccessLevelType ?? null,
      organizations,
    });
  } catch (err) {
    return res.status(500).json({ error: err.message });
  }
});

export default router;
