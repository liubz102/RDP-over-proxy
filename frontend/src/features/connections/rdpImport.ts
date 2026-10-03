import { ProfileService, RDP_MAX_SIZE, type RDPImportView } from "../../api/backend";
import { toBase64 } from "../../lib/base64";

/** An .rdp file read into a new profile, with the name of the file it came from. */
export interface Imported {
  file: string;
  view: RDPImportView;
}

/**
 * Reads an .rdp file the user chose into a draft profile (the Go side
 * parses it; nothing is stored). A file too large to be an .rdp file is
 * turned down here already, rather than sent over.
 */
export async function importRdp(file: File): Promise<Imported> {
  if (file.size > RDP_MAX_SIZE) {
    throw new Error("the file is too large to be an .rdp file", {
      cause: { code: "rdp.tooLarge", message: "the file is too large to be an .rdp file" },
    });
  }
  const data = new Uint8Array(await file.arrayBuffer());
  return { file: file.name, view: await ProfileService.ParseRDP(file.name, toBase64(data)) };
}
