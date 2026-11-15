export function parse(text: string): string {
  try {
    return JSON.parse(text) as string;
  }
  // ruleid: typescript-empty-catch
  catch (error) {}
}
