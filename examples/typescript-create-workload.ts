/**
 * Submit a single bandwidth workload, wait for it to finish, print the
 * billable cost.
 *
 * Run with:
 *   pnpm tsx examples/typescript-create-workload.ts
 *
 * Requires IOGRID_API_KEY in the environment.
 */
import { IogridClient, IogridError } from '@iogrid/sdk';

async function main() {
  const apiKey = process.env.IOGRID_API_KEY;
  if (!apiKey) throw new Error('set IOGRID_API_KEY first');
  const iogrid = new IogridClient({ apiKey });

  const w = await iogrid.createWorkload({
    type: 'BANDWIDTH',
    priority: 'NORMAL',
    bandwidth: {
      targetUrl: 'https://example.com/product/42',
      method: 'GET',
      preferredRegion: 'us-east-1',
      category: 'e_commerce',
    },
    labels: { example: 'create-workload-ts' },
  });
  console.log('submitted', w.id);

  // Tail events to terminal.
  try {
    for await (const ev of iogrid.streamWorkloadEvents(w.id)) {
      console.log(ev.occurredAt, ev.newStatus, ev.note ?? '');
    }
  } catch (err) {
    if (err instanceof IogridError) console.error('stream failed:', err.code, err.message);
    else throw err;
  }

  const final = await iogrid.getWorkload(w.id);
  console.log('terminal:', final.result?.terminalStatus, 'cost:', final.result?.cost);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
