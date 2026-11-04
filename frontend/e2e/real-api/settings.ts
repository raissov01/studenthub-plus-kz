export const botToken = '123456789:local-playwright-only-not-a-real-bot';
export const adminTelegramID = 990000001;
// café Kaspi: the fake sidecar of the real-API suite (e2e/real-api/fake-kaspi.ts); local test values only
export const kaspiPort = Number(process.env.E2E_KASPI_PORT || 4322);
export const kaspiApiKey = 'local-playwright-kaspi-internal-key-0123456789';
export const kaspiSessionKey = 'ab'.repeat(32);
