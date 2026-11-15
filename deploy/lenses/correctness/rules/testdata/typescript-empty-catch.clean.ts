export function parse(text: string): string {
  try {
    return JSON.parse(text) as string;
  } catch (error) {
    throw new Error(`cannot parse the body: ${String(error)}`);
  }
}
