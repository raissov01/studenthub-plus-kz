import crypto from 'crypto';
import {ecKeyPair} from './config.js';


// ─── ECDH ───

const vtokenSuite = 'OCRA-1:HOTP-SHA256-6:QH64-T1M';

// ─── AES-256-GCM encryption for vtokenSecret ───

if (!process.env.TOKEN_SECRET_KEY) {
  console.error('FATAL: TOKEN_SECRET_KEY environment variable is not set.');
  console.error('Generate one with: echo "TOKEN_SECRET_KEY=$(openssl rand -hex 32)" > .env');
  process.exit(1);
}
const ENCRYPTION_KEY = Buffer.from(process.env.TOKEN_SECRET_KEY, 'hex');

export const encryptSecret = (secretBuffer) => {
  const iv = crypto.randomBytes(12);
  const cipher = crypto.createCipheriv('aes-256-gcm', ENCRYPTION_KEY, iv);
  const encrypted = Buffer.concat([cipher.update(secretBuffer), cipher.final()]);
  const tag = cipher.getAuthTag();
  return Buffer.concat([iv, tag, encrypted]).toString('base64');
};

export const decryptSecret = (tokenB64) => {
  const buf = Buffer.from(tokenB64, 'base64');
  const iv = buf.subarray(0, 12);
  const tag = buf.subarray(12, 28);
  const encrypted = buf.subarray(28);
  const decipher = crypto.createDecipheriv('aes-256-gcm', ENCRYPTION_KEY, iv);
  decipher.setAuthTag(tag);
  return Buffer.concat([decipher.update(encrypted), decipher.final()]);
};

// ECDH keypairs of in-flight logins, keyed by Kaspi processId. Kept only in memory: a global
// "last keypair" (and a shared ecdh-keypair.json) let two concurrent logins overwrite each other.
const ecdhByProcess = new Map();
export const ECDH_TTL_MS = 10 * 60 * 1000;

export const generateECDH = (processId) => {
  if (!processId) throw new Error('processId required for ECDH');
  const keyPair = crypto.generateKeyPairSync('ec', {namedCurve: 'prime256v1'});
  ecdhByProcess.set(processId, {keyPair, createdAt: Date.now()});
  return keyPair.publicKey.export({type: 'spki', format: 'der'}).toString('base64');
};

export const completeECDH = (processId, serverX509B64) => {
  const entry = ecdhByProcess.get(processId);
  if (!entry) throw new Error('No ECDH keypair for this processId');
  ecdhByProcess.delete(processId);
  const serverPubKey = crypto.createPublicKey({
    key: Buffer.from(serverX509B64, 'base64'),
    format: 'der',
    type: 'spki',
  });
  return crypto.diffieHellman({privateKey: entry.keyPair.privateKey, publicKey: serverPubKey});
};

export const discardECDH = (processId) => ecdhByProcess.delete(processId);

/** Drops keypairs of logins that never finished. Returns how many were removed. */
export const purgeStaleECDH = (now = Date.now()) => {
  let n = 0;
  for (const [id, e] of ecdhByProcess) {
    if (now - e.createdAt > ECDH_TTL_MS) {
      ecdhByProcess.delete(id);
      n++;
    }
  }
  return n;
};

export const pendingECDHCount = () => ecdhByProcess.size;

// ─── Helpers ───

const hexToBytes = (hex) => {
  const bytes = [];
  for (let i = 0; i < hex.length; i += 2) {
    bytes.push(parseInt(hex.substring(i, i + 2), 16));
  }
  return Buffer.from(bytes);
};

// ─── OCRA-1 TOTP (matches Kaspi vtoken) ───

export const computeTokenSnMac = (tokenSN, secret) => {
  if (!secret) return '000000';

  const timeStep = BigInt(Date.now()) / BigInt(30000);
  const timeHex = timeStep.toString(16);

  const qHex = Buffer.from(tokenSN || '00000000')
    .toString('hex')
    .substring(0, 64);

  const suiteBytes = Buffer.from(vtokenSuite);
  const separator = Buffer.from([0x00]);

  const qPadded = qHex.padEnd(256, '0');
  const qBytes = hexToBytes(qPadded);

  const tPadded = timeHex.padStart(16, '0');
  const tBytes = hexToBytes(tPadded);

  const dataBuffer = Buffer.concat([suiteBytes, separator, qBytes, tBytes]);

  const hash = crypto.createHmac('sha256', secret).update(dataBuffer).digest();

  // Dynamic truncation (RFC 4226)
  const offset = hash[hash.length - 1] & 0x0f;
  const binCode =
    ((hash[offset] & 0x7f) << 24) |
    ((hash[offset + 1] & 0xff) << 16) |
    ((hash[offset + 2] & 0xff) << 8) |
    (hash[offset + 3] & 0xff);

  return (binCode % 1000000).toString().padStart(6, '0');
};

// ─── ECDSA signing ───

export const ecSign = (data) => {
  const sign = crypto.createSign('SHA256');
  sign.update(data);
  sign.end();
  return sign.sign(ecKeyPair.privateKey).toString('base64');
};

export const signDataPayload = (dataB64) => ecSign(dataB64);

export const computeXSU = (url) => crypto.createHash('md5').update(url.toLowerCase()).digest('hex');

export const computeXSign = (url, headers, xshList, body) => {
  const keys = xshList.split(',');
  const lines = [];
  for (const name of keys) {
    if (name === 'url') {
      lines.push('url:' + url.toLowerCase());
    } else {
      lines.push(name.toLowerCase() + ':' + (headers[name] || ''));
    }
  }
  let signText = lines.join('\n');
  if (body) {
    signText += '\n' + body;
  }
  const hash = crypto.createHash('sha256').update(signText, 'utf8').digest();
  return ecSign(hash);
};
