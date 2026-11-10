// The only tool an AI agent student uses: one command per call.
//
//     npx tsx harness/student.ts NAME open
//     npx tsx harness/student.ts NAME look
//     npx tsx harness/student.ts NAME click "56"
//     npx tsx harness/student.ts NAME type "Олена"
//     npx tsx harness/student.ts NAME wait 10
//     npx tsx harness/student.ts NAME reload
//     npx tsx harness/student.ts NAME offline 8
//     npx tsx harness/student.ts NAME close
//
// Each call prints the step number, the time, and what is on the screen after
// the command. The driver records the same step on its side.

const API = process.env.HARNESS_API ?? 'http://127.0.0.1:8198';

const USAGE = `The only tool an AI agent student uses: one command per call.

    npx tsx harness/student.ts NAME open
    npx tsx harness/student.ts NAME look
    npx tsx harness/student.ts NAME click "56"
    npx tsx harness/student.ts NAME type "Олена"
    npx tsx harness/student.ts NAME wait 10
    npx tsx harness/student.ts NAME reload
    npx tsx harness/student.ts NAME offline 8
    npx tsx harness/student.ts NAME close
`;

async function main(argv: string[]): Promise<number> {
  if (argv.length < 2) {
    console.log(USAGE);
    return 2;
  }
  const [name, cmd] = argv;
  const arg = argv.length > 2 ? argv.slice(2).join(' ') : null;
  try {
    const response = await fetch(API + '/cmd', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ student: name, cmd, arg }),
      signal: AbortSignal.timeout(150_000),
    });
    console.log(await response.text());
    return response.ok ? 0 : 1;
  } catch (error) {
    const cause = error instanceof Error && error.cause instanceof Error ? error.cause.message : String(error);
    console.log(`The classroom is not running (${cause}).`);
    return 1;
  }
}

process.exit(await main(process.argv.slice(2)));
export {};
