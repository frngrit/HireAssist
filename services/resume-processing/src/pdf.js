// Turn a resume PDF into plain text. Deterministic and local: the AI Service
// never receives a file (see ai_service.proto, CandidateProfile).
// unpdf wraps Mozilla's pdf.js, so Thai and English text both come out.

class UnparsableResumeError extends Error {}

let unpdf; // ES module, loaded on first use
async function lib() {
  unpdf = unpdf || (await import("unpdf"));
  return unpdf;
}

async function extractText(buffer) {
  const { getDocumentProxy, extractText: extract } = await lib();
  let text;
  try {
    const pdf = await getDocumentProxy(new Uint8Array(buffer));
    ({ text } = await extract(pdf, { mergePages: true }));
  } catch (err) {
    throw new UnparsableResumeError(`Not a readable PDF: ${err.message}`);
  }
  text = (text || "").replace(/[ \t]+\n/g, "\n").replace(/\n{3,}/g, "\n\n").trim();
  // A scanned resume has pages but no text layer: flag it for manual review
  // (FR-2.7) instead of sending an empty string to the model.
  if (text.length < 20) {
    throw new UnparsableResumeError("The PDF has no extractable text (scanned image?) — needs manual review");
  }
  return text;
}

module.exports = { extractText, UnparsableResumeError };
