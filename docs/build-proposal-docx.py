#!/usr/bin/env python3
"""Build a short Word proposal.

Project Name, Problem Description, Target Customers, Scenario (use-case &
description), Functional Requirements, Non-functional Requirements — the
brief's headings, in the brief's order.

The prose sections are a condensation of PROPOSAL.md, not a conversion — the
proposal is written to be complete, this is written to be read in one sitting.
PROPOSAL.md stays the source of truth; when the two disagree, it wins.

The requirements are the exception: they are parsed out of
FUNCTIONAL-REQUIREMENTS.md and NON-FUNCTIONAL-REQUIREMENTS.md verbatim, so
they cannot drift from the documents that own them. Editing a requirement here
would be editing it in the wrong place.
"""
import pathlib
import re
import os

from docx import Document
from docx.enum.section import WD_ORIENT, WD_SECTION
from docx.enum.table import WD_TABLE_ALIGNMENT
from docx.enum.text import WD_ALIGN_PARAGRAPH
from docx.oxml import OxmlElement
from docx.oxml.ns import qn
from docx.shared import Inches, Pt, RGBColor

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUT = os.path.join(REPO, "docs", "HireAssist-Proposal.docx")
FR_SRC = os.path.join(REPO, "docs", "FUNCTIONAL-REQUIREMENTS.md")
NFR_SRC = os.path.join(REPO, "docs", "NON-FUNCTIONAL-REQUIREMENTS.md")
UC_DIAGRAM = os.path.join(REPO, "docs", "diagrams", "use-case-diagram.png")
ARCH_DIAGRAM = os.path.join(REPO, "docs", "diagrams", "sw-arch-v2.png")
SOC_SRC = os.path.join(REPO, "docs", "SERVICE-OPERATIONS-COLLABORATORS.md")
PROPOSAL = os.path.join(REPO, "docs", "PROPOSAL.md")

INK = RGBColor(0x1A, 0x1A, 0x1A)
ACCENT = RGBColor(0x1B, 0x5E, 0x8C)
MUTED = RGBColor(0x55, 0x5F, 0x6B)
RULE = "D8DDE3"
BODY = "Calibri"
THAI = "Sarabun"


def shade(el, fill):
    sh = OxmlElement("w:shd")
    sh.set(qn("w:val"), "clear")
    sh.set(qn("w:fill"), fill)
    el.append(sh)


def thai(run):
    """Thai needs a Thai-capable face on the complex-script slot too, or Word
    substitutes per-glyph and the line looks broken."""
    rpr = run._element.get_or_add_rPr()
    fonts = rpr.find(qn("w:rFonts"))
    if fonts is None:
        fonts = OxmlElement("w:rFonts")
        rpr.append(fonts)
    for slot in ("w:ascii", "w:hAnsi", "w:cs"):
        fonts.set(qn(slot), THAI)
    rpr_sz = OxmlElement("w:szCs")
    rpr_sz.set(qn("w:val"), "22")
    rpr.append(rpr_sz)


def para(doc, text="", *, size=10.5, bold=False, italic=False, color=INK,
         before=0, after=6, indent=0.0, align=None):
    p = doc.add_paragraph()
    p.paragraph_format.space_before = Pt(before)
    p.paragraph_format.space_after = Pt(after)
    p.paragraph_format.left_indent = Inches(indent)
    if align is not None:
        p.alignment = align
    if text:
        r = p.add_run(text)
        r.font.name = BODY
        r.font.size = Pt(size)
        r.font.color.rgb = color
        r.bold = bold
        r.italic = italic
    return p


def rich(doc, chunks, *, size=10.5, before=0, after=6, indent=0.0):
    """chunks: list of (text, bold, italic) — one paragraph, mixed emphasis."""
    p = doc.add_paragraph()
    p.paragraph_format.space_before = Pt(before)
    p.paragraph_format.space_after = Pt(after)
    p.paragraph_format.left_indent = Inches(indent)
    for text, bold, italic in chunks:
        r = p.add_run(text)
        r.font.name = BODY
        r.font.size = Pt(size)
        r.font.color.rgb = INK
        r.bold = bold
        r.italic = italic
    return p


def heading(doc, text, first=False):
    p = doc.add_paragraph()
    p.paragraph_format.space_before = Pt(2 if first else 16)
    p.paragraph_format.space_after = Pt(7)
    r = p.add_run(text)
    r.font.name = BODY
    r.font.size = Pt(14)
    r.bold = True
    r.font.color.rgb = ACCENT
    pbdr = OxmlElement("w:pBdr")
    bottom = OxmlElement("w:bottom")
    bottom.set(qn("w:val"), "single")
    bottom.set(qn("w:sz"), "6")
    bottom.set(qn("w:space"), "3")
    bottom.set(qn("w:color"), RULE)
    pbdr.append(bottom)
    p._p.get_or_add_pPr().append(pbdr)
    return p


def bullet(doc, label, text):
    p = doc.add_paragraph(style="List Bullet")
    p.paragraph_format.space_after = Pt(3)
    p.paragraph_format.left_indent = Inches(0.28)
    if label:
        r = p.add_run(label)
        r.font.name = BODY
        r.font.size = Pt(10.5)
        r.bold = True
        r.font.color.rgb = INK
        s = p.add_run(" — ")
        s.font.name = BODY
        s.font.size = Pt(10.5)
        s.font.color.rgb = MUTED
    r = p.add_run(text)
    r.font.name = BODY
    r.font.size = Pt(10.5)
    r.font.color.rgb = INK
    return p


def callout(doc, lines):
    """A bordered pull-quote: the one statement the section turns on."""
    p = doc.add_paragraph()
    p.paragraph_format.space_before = Pt(6)
    p.paragraph_format.space_after = Pt(8)
    p.paragraph_format.left_indent = Inches(0.22)
    p.paragraph_format.right_indent = Inches(0.22)
    for i, (text, is_thai, italic) in enumerate(lines):
        if i:
            p.add_run().add_break()
        r = p.add_run(text)
        r.font.name = THAI if is_thai else BODY
        r.font.size = Pt(12 if is_thai else 10.5)
        r.bold = not italic
        r.italic = italic
        r.font.color.rgb = MUTED if italic else INK
        if is_thai:
            thai(r)
    ppr = p._p.get_or_add_pPr()
    pbdr = OxmlElement("w:pBdr")
    left = OxmlElement("w:left")
    left.set(qn("w:val"), "single")
    left.set(qn("w:sz"), "18")
    left.set(qn("w:space"), "10")
    left.set(qn("w:color"), "1B5E8C")
    pbdr.append(left)
    ppr.append(pbdr)
    shade(ppr, "F4F7F9")
    return p


def table(doc, headers, rows, widths):
    t = doc.add_table(rows=1, cols=len(headers))
    t.style = "Table Grid"
    t.autofit = False
    for i, (cell, head) in enumerate(zip(t.rows[0].cells, headers)):
        cell.width = Inches(widths[i])
        shade(cell._tc.get_or_add_tcPr(), "EDF2F6")
        p = cell.paragraphs[0]
        p.paragraph_format.space_before = Pt(3)
        p.paragraph_format.space_after = Pt(3)
        r = p.add_run(head)
        r.font.name = BODY
        r.font.size = Pt(9.5)
        r.bold = True
        r.font.color.rgb = ACCENT
    for row in rows:
        cells = t.add_row().cells
        for i, value in enumerate(row):
            cells[i].width = Inches(widths[i])
            p = cells[i].paragraphs[0]
            p.paragraph_format.space_before = Pt(3)
            p.paragraph_format.space_after = Pt(3)
            r = p.add_run(value)
            r.font.name = BODY
            r.font.size = Pt(9.5)
            r.font.color.rgb = INK
            r.bold = i == 0
    return t


def plain(md):
    """Markdown inline formatting to plain text. The requirement tables carry
    bold, code spans and links that mean nothing in a Word table cell."""
    md = re.sub(r"\[([^\]]+)\]\([^)]*\)", r"\1", md)
    md = md.replace("**", "").replace("`", "")
    return re.sub(r"\s+", " ", md).strip()


def parse_reqs(path, prefix, ncols):
    """Pull requirement rows out of a markdown file, grouped by its `## ` headings.

    Parsed rather than retyped so the export cannot drift from the document
    that owns the requirements.

    A row must open with exactly one id. The NFR file ends with an
    "architecturally significant" table whose first cell lists several
    ("NFR-02, NFR-06, NFR-08"); matching on the prefix alone swept those four
    rows in as if they were requirements and made the count read 21 instead
    of 17.
    """
    row_re = re.compile(rf"^\|\s*{prefix}[\d.]+\s*\|")
    groups, current, rows = [], None, []
    for line in pathlib.Path(path).read_text().splitlines():
        if line.startswith("## "):
            if current and rows:
                groups.append((current, rows))
            current, rows = plain(line[3:]), []
        elif row_re.match(line):
            cells = [plain(c) for c in line.strip().strip("|").split(" | ")]
            if len(cells) < ncols:
                raise ValueError(f"{path}: expected {ncols} columns in {line[:40]!r}")
            rows.append(cells[:ncols])
    if current and rows:
        groups.append((current, rows))
    return groups


def parse_members(path):
    """The group members table out of PROPOSAL.md, read rather than retyped —
    a student id mistyped into a build script is a mistake nobody proofreads."""
    rows, inside = [], False
    for line in pathlib.Path(path).read_text().splitlines():
        if line.startswith("## "):
            inside = line[3:].strip() == "Group Members"
            continue
        if inside and re.match(r"^\|\s*\d+\s*\|", line):
            cells = [plain(c) for c in line.strip().strip("|").split(" | ")]
            rows.append(cells[:3])          # no nickname column in the document
    if not rows:
        raise ValueError(f"{path}: no group members found")
    return rows


def subhead(doc, text, before=10):
    p = doc.add_paragraph()
    p.paragraph_format.space_before = Pt(before)
    p.paragraph_format.space_after = Pt(3)
    p.paragraph_format.keep_with_next = True
    r = p.add_run(text)
    r.font.name = BODY
    r.font.size = Pt(10.5)
    r.bold = True
    r.font.color.rgb = INK
    return p


def uc_block(doc, title, actor, goal, steps):
    subhead(doc, title)
    rich(doc, [("Actor: ", True, False), (actor, False, False),
               ("   ·   Goal: ", True, False), (goal, False, True)],
         size=9.5, after=3, indent=0.0)
    p = doc.add_paragraph()
    p.paragraph_format.space_after = Pt(2)
    p.paragraph_format.left_indent = Inches(0.05)
    for i, step in enumerate(steps, start=1):
        if i > 1:
            p.add_run("  ")
        n = p.add_run(f"{i}. ")
        n.font.name = BODY
        n.font.size = Pt(9.5)
        n.bold = True
        n.font.color.rgb = ACCENT
        r = p.add_run(step)
        r.font.name = BODY
        r.font.size = Pt(9.5)
        r.font.color.rgb = INK
    return p


def diagram(doc, image_path, width_in):
    """Place the rendered diagram.

    A missing image raises rather than warning. An earlier version skipped it
    with a printed warning, and when the diagram was switched from SVG to PNG
    the export silently shipped without a use case diagram at all — the kind
    of failure nobody sees until a marker opens the file.
    """
    if not os.path.exists(image_path):
        raise FileNotFoundError(
            f"{image_path} is missing. Re-render it with "
            "`plantuml -tpng docs/diagrams/use-case-diagram.puml`."
        )
    p = doc.add_paragraph()
    p.alignment = WD_ALIGN_PARAGRAPH.CENTER
    p.paragraph_format.space_before = Pt(4)
    p.paragraph_format.space_after = Pt(2)
    p.add_run().add_picture(image_path, width=Inches(width_in))
    return p


SOC_INLINE = re.compile(r"(\*\*.+?\*\*|`[^`]+`|\[[^\]]+\]\([^)]*\)|\*[^*\n]+?\*)")


def soc_runs(par, text, *, italic=False, bold=False):
    """Inline markdown inside one Service-Operations-Collaborators cell."""
    for tok in SOC_INLINE.split(text):
        if not tok:
            continue
        if tok.startswith("**") and tok.endswith("**") and len(tok) > 4:
            soc_runs(par, tok[2:-2], italic=italic, bold=True)
            continue
        if tok.startswith("*") and tok.endswith("*") and len(tok) > 2:
            soc_runs(par, tok[1:-1], italic=True, bold=bold)
            continue
        if tok.startswith("[") and "](" in tok:
            soc_runs(par, tok[1:tok.index("](")], italic=italic, bold=bold)
            continue
        r = par.add_run(tok)
        if tok.startswith("`") and tok.endswith("`") and len(tok) > 2:
            r.text = tok[1:-1]
            r.font.name = "Consolas"
            r.font.size = Pt(7.5)
            r.font.color.rgb = ACCENT
        else:
            r.font.name = BODY
            r.font.size = Pt(8)
            r.font.color.rgb = INK
        r.italic = italic
        r.bold = bold


def _balance(md):
    """An emphasis span may straddle a <br>; close and reopen it on each line
    so that splitting on <br> never leaves an unpaired marker."""
    def fix(m, mark):
        parts = m.group(1).split("<br>")
        return "<br>".join(f"{mark}{q}{mark}" if q.strip() else q for q in parts)
    md = re.sub(r"\*\*(.+?)\*\*", lambda m: fix(m, "**"), md, flags=re.S)
    md = re.sub(r"(?<!\*)\*([^*]+?)\*(?!\*)", lambda m: fix(m, "*"), md, flags=re.S)
    return md


def parse_soc(path):
    """The Service-Operations-Collaborators table, read rather than retyped."""
    rows = []
    for line in pathlib.Path(path).read_text().splitlines():
        t = line.strip()
        if t.startswith("|") and t.endswith("|"):
            if re.match(r"^\|[\s:\-\|]+\|$", t):
                continue
            rows.append([c.strip() for c in t.strip("|").split("|")])
        elif rows:
            break                       # first table only
    if not rows or len(rows[0]) != 3:
        raise ValueError(f"{path}: table not found, or it no longer has three columns")
    return rows


def soc_table(doc, rows, widths):
    t = doc.add_table(rows=len(rows), cols=3)
    t.style = "Table Grid"
    t.alignment = WD_TABLE_ALIGNMENT.CENTER
    t.autofit = False
    for i, row in enumerate(rows):
        for j in range(3):
            c = t.cell(i, j)
            c.width = Inches(widths[j])
            c.text = ""
            if i == 0:
                shade(c._tc.get_or_add_tcPr(), "EDF2F6")
                par = c.paragraphs[0]
                par.paragraph_format.space_before = Pt(3)
                par.paragraph_format.space_after = Pt(3)
                r = par.add_run(row[j])
                r.font.name = BODY
                r.font.size = Pt(9)
                r.bold = True
                r.font.color.rgb = ACCENT
                continue
            md = _balance(row[j].replace("\u3000", "   "))
            for k, line in enumerate(md.split("<br>")):
                par = c.paragraphs[0] if k == 0 else c.add_paragraph()
                par.paragraph_format.space_before = Pt(0)
                par.paragraph_format.space_after = Pt(0)
                par.paragraph_format.line_spacing = 1.05
                if line.strip():
                    soc_runs(par, line.strip())
                else:
                    par.add_run("").font.size = Pt(4)
    trPr = t.rows[0]._tr.get_or_add_trPr()      # repeat the header across pages
    hdr = OxmlElement("w:tblHeader")
    hdr.set(qn("w:val"), "true")
    trPr.append(hdr)
    return t


doc = Document()
section = doc.sections[0]
section.top_margin = section.bottom_margin = Inches(0.8)
section.left_margin = section.right_margin = Inches(0.9)
normal = doc.styles["Normal"]
normal.font.name = BODY
normal.font.size = Pt(10.5)

# ── Title ───────────────────────────────────────────────────────────────────
p = para(doc, "HireAssist", size=26, bold=True, color=ACCENT, after=0)
para(doc, "Project Proposal  ·  Software Architecture Term Project",
     size=10.5, italic=True, color=MUTED, after=14)

# ── Project Name ────────────────────────────────────────────────────────────
heading(doc, "Project Name", first=True)
rich(doc, [
    ("HireAssist", True, False),
    (" — an AI-assisted hiring-support layer for companies that receive more applications "
     "than they can screen carefully and interview well.", False, False),
], after=8)
rich(doc, [
    ("It is ", False, False), ("not", True, False),
    (" a job board, ", False, False), ("not", True, False),
    (" a job-application platform, and ", False, False), ("not", True, False),
    (" another applicant-tracking system. It attaches to the hiring channels a company "
     "already uses and supplies the one thing they lack: the capacity to judge every "
     "application properly.", False, False),
], after=8)
rich(doc, [
    ("The human still decides. HireAssist makes sure that when they decide, they are "
     "deciding on evidence they actually had time to look at.", False, True),
], after=4)

subhead(doc, "Group Members", before=12)
table(doc,
      ["#", "Student ID", "Name"],
      parse_members(PROPOSAL),
      [0.5, 1.6, 3.6])

# ── Problem Description ─────────────────────────────────────────────────────
heading(doc, "Problem Description")
callout(doc, [
    ("บริษัทขนาดเล็กที่มีพนักงานไม่เพียงพอ ไม่สามารถ screen resume ได้ทัน", True, False),
    ("Small companies with insufficient staff cannot screen incoming resumes fast enough.",
     False, True),
])
para(doc,
     "Hiring is bursty. A position goes up on JobsDB, JobThai or LinkedIn and applications "
     "arrive in a concentrated wave into an inbox or a spreadsheet. Every one must be opened, "
     "read and judged by hand, and each surviving candidate interviewed by someone who has "
     "actually studied their resume against the role. The people doing that work are rarely "
     "dedicated to it — a single HR generalist who also owns payroll and onboarding, or a "
     "founder hiring between their actual job.")
rich(doc, [
    ("The decisive factor is not headcount but a ", False, False),
    ("ratio", True, False),
    (": applications arriving against the capacity to judge them properly. Screening effort "
     "scales with applicant volume; screening capacity scales with headcount, and headcount "
     "never keeps up. One recruiter with 80 applicants and five recruiters with 2,000 have "
     "the same problem.", False, False),
], after=8)
para(doc, "When that ratio breaks, quality collapses in two places:", after=4)
bullet(doc, "Screening becomes shallow",
       "resumes get a 20-second skim or a crude keyword filter, so good candidates are "
       "missed and weak ones advance.")
bullet(doc, "Interviews become unprepared",
       "the interviewer opens the resume minutes beforehand and falls back on generic "
       "questions, so the hour fails to verify the very things screening could not confirm.")
para(doc, "", after=4)
para(doc, "What that costs the company", size=10.5, bold=True, after=4)
bullet(doc, "Lost candidates",
       "strong applicants are hired by a faster competitor, and the rest are ghosted — "
       "which damages employer brand in a small, highly-networked local market.")
bullet(doc, "Decisions with no trail",
       "rejections live in someone's head, so nobody can answer why a candidate was passed "
       "over, or check whether screening is consistent.")
bullet(doc, "A dead archive and rotting positions",
       "past applicants are never revisited when a similar role reopens, and a requisition "
       "open for 90 days looks exactly like one opened last week.")
bullet(doc, "PDPA exposure",
       "resumes are personal data. Keeping them indefinitely in an inbox with no retention "
       "limit is a compliance risk SMEs are least equipped to manage.")
para(doc, "", after=4)
para(doc, "Why the existing options do not solve it", size=10.5, bold=True, after=4)
bullet(doc, "Enterprise ATS",
       "built to track applications, not to judge them — and priced for companies with a "
       "recruitment department.")
bullet(doc, "Job boards",
       "they solve candidate supply, and in doing so make this problem worse: more resumes "
       "into the same inbox.")
bullet(doc, "Keyword filters and CV parsers",
       "mechanical. They match strings rather than weighing evidence, cannot explain a "
       "decision, and reject good candidates for using different vocabulary.")
bullet(doc, "Spreadsheets and inbox",
       "the current reality for most SMEs, and exactly the thing that is failing.")
para(doc, "", after=4)
rich(doc, [
    ("The gap is not candidate supply and not record-keeping. It is ", False, False),
    ("judgement capacity", True, False),
    (" after the applications arrive — which is what HireAssist supplies: explainable "
     "screening at volume, interview questions grounded in the specific resume and role, "
     "re-matching of the existing talent pool, visibility into stalling positions, and "
     "candidate data kept compliant.", False, False),
], after=4)

# ── Target Customers ────────────────────────────────────────────────────────
heading(doc, "Target Customers")
callout(doc, [
    ("Any company that receives more applications than it can screen carefully and "
     "interview well.", False, False),
])
rich(doc, [
    ("The customer is defined by a ratio, not by company size.", True, False),
    (" A 20-person startup with one HR generalist and 80 applicants per opening, and a "
     "300-person company with three recruiters and 2,000, are the same customer. A company "
     "qualifies when application volume exceeds screening capacity; when the role needs "
     "judgement rather than a checkbox filter; when interviews are not systematically "
     "prepared; when similar roles recur, so past applicants keep their value; and when "
     "hiring runs on an inbox and spreadsheets rather than an adequate ATS.", False, False),
], after=6)
rich(doc, [
    ("Being small is not enough to qualify.", False, True),
    (" A 15-person company hiring one person a year has no throughput problem.", False, True),
], after=10)

para(doc, "Primary segment (initial focus)", size=10.5, bold=True, after=4)
rich(doc, [
    ("Thai technology companies — software houses, product startups, digital agencies and "
     "IT service firms, roughly 10 to 200 employees.", True, False),
], after=6)
table(doc,
      ["Attribute", "Profile"],
      [
          ["Hiring pattern", "Bursty and urgent; several roles at once, and the same roles "
                             "reopen every few months"],
          ["Volume", "~20–200 applications per opening, in concentrated waves after a posting"],
          ["Capacity", "0–2 HR generalists, often helped by a tech lead hiring between "
                       "their actual work"],
          ["Existing tools", "Inbox, Google Sheets, LINE, a job board or two; rarely a real ATS"],
          ["Languages", "Mixed Thai and English resumes; titles and technical skills "
                        "mostly English"],
      ],
      [1.25, 5.45])
para(doc, "", after=4)
rich(doc, [
    ("Why this segment first:", True, False),
    (" tech resumes are comparatively structured, which makes explainable matching "
     "tractable; technical roles are exactly where a generic interview wastes the hour; and "
     "roles recur, so the talent pool pays off from the first re-match. This is a beachhead, "
     "not a boundary — nothing in the design assumes a small company.", False, False),
], after=10)

para(doc, "Users of the system", size=10.5, bold=True, after=4)
table(doc,
      ["User", "Goal", "What they get"],
      [
          ["Recruiter / HR",
           "Clear the backlog, shortlist candidates who can be defended, interview them well",
           "A ranked, explainable shortlist instead of a folder of unread PDFs; questions "
           "grounded in that resume and role; alerts when a position stalls"],
          ["Admin / owner",
           "Keep hiring compliant, consistent and auditable, and control access",
           "Member and role management, retention settings, and a recorded reason behind "
           "every decision"],
          ["Candidate (indirect)",
           "Be judged on evidence, and have their data kept no longer than needed",
           "A real reading rather than a 20-second skim; data that ages out on a defined "
           "schedule, and an erasure request that is honoured"],
      ],
      [1.25, 2.1, 3.35])

# ── Scenario (use-case & description) ───────────────────────────────────────
doc.add_page_break()
heading(doc, "Scenario (use-case & description)", first=True)
callout(doc, [
    ("Describe the role → drop in the resumes → get an explained shortlist → walk into the "
     "interview prepared → and never lose the candidates you passed on.", False, False),
])
para(doc,
     "HireAssist is used by a small hiring team through one recruiter-facing web application. "
     "Seven use cases cover that flow; UC-0 and UC-6 are foundations, UC-1 to UC-5 are the "
     "hiring work itself.", after=8)

table(doc,
      ["ID", "Use case", "Primary actor", "Consequence it addresses"],
      [
          ["UC-0", "Sign in", "Guest", "— (foundation)"],
          ["UC-1", "Create a job opening from natural-language requirements", "Recruiter",
           "Slow time-to-screen"],
          ["UC-2", "Batch-screen resumes against a job opening", "Recruiter",
           "Slow time-to-screen, ghosted candidates, no decision trail"],
          ["UC-3", "Generate candidate-specific interview questions", "Recruiter",
           "Unprepared, generic interviews"],
          ["UC-4", "Monitor hiring pipeline and stale positions", "Recruiter / System Scheduler",
           "Positions rot silently"],
          ["UC-5", "Enforce candidate data retention", "System Scheduler / Admin",
           "PDPA exposure"],
          ["UC-6", "Manage members and roles", "Admin", "— (foundation)"],
      ],
      [0.5, 2.5, 1.3, 2.4])

subhead(doc, "Actors", before=12)
table(doc,
      ["Actor", "Description"],
      [
          ["Guest", "Anyone who has reached HireAssist but has not signed in. On signing in "
                    "they act as a Recruiter or an Admin, per the role their account holds."],
          ["Recruiter", "A signed-in member running day-to-day hiring — an HR generalist, a "
                        "founder, or a tech lead hiring for their own team. Primary actor of "
                        "UC-1 to UC-4."],
          ["Admin", "The signed-in owner of the installation. Every Recruiter capability, plus "
                    "the retention policy (UC-5) and members and roles (UC-6)."],
          ["Candidate", "Indirect actor. Does not log in and has no interface. Supplies the "
                        "resume and is the subject of the data processed; an erasure request "
                        "arrives out of band and an Admin executes it."],
          ["System Scheduler", "Supporting actor. Time-driven trigger for work no human "
                               "initiates: staleness checks (UC-4) and retention enforcement "
                               "(UC-5)."],
      ],
      [1.25, 5.45])

subhead(doc, "Use case diagram", before=12)
diagram(doc, UC_DIAGRAM, 6.0)
para(doc, "UML use case diagram.",
     size=8.5, italic=True, color=MUTED, align=WD_ALIGN_PARAGRAPH.CENTER, after=6)

subhead(doc, "The use cases", before=12)
uc_block(doc, "UC-0 — Sign in", "Guest",
         "Enter the system with the permissions of their role.",
         ["Guest signs in with email and password.",
          "System authenticates and issues a session token carrying their role.",
          "The user acts as a Recruiter or an Admin accordingly.",
          "Every later request is authorised at the gateway against that token."])

uc_block(doc, "UC-1 — Create a job opening from natural-language requirements", "Recruiter",
         "Turn the way a role is actually described in conversation into structured, "
         "machine-usable criteria — without filling in a long form.",
         ["Recruiter pastes a free-text description, Thai or English.",
          "System extracts a proposed title and a weighted, categorised criteria set.",
          "Recruiter reviews and edits criteria, weights and must-have flags.",
          "Recruiter sets expected time-to-fill and confirms.",
          "System stores the opening as open and reports it to the pipeline dashboard."])

uc_block(doc, "UC-2 — Batch-screen resumes against a job opening", "Recruiter",
         "Convert a pile of unread resumes into a ranked shortlist with a stated reason for "
         "every ranking.",
         ["Recruiter uploads a batch of PDF resumes against an open job opening.",
          "System validates the files, creates a screening batch and returns a batch ID "
          "immediately.",
          "System parses each resume into a normalised candidate profile.",
          "System scores each profile against the criteria and writes a justification.",
          "Results stream into the ranked shortlist as they complete.",
          "Recruiter overrides any score the AI got wrong and shortlists candidates.",
          "System records each decision and adds every candidate to the talent pool with the "
          "date their data was collected."])

uc_block(doc, "UC-3 — Generate candidate-specific interview questions", "Recruiter",
         "Walk into an interview with questions grounded in this candidate's resume and this "
         "job's requirements.",
         ["Recruiter opens a shortlisted candidate and requests an interview guide.",
          "System retrieves the candidate profile, the resume and the job criteria.",
          "System generates questions covering resume claims, must-have gaps and role depth, "
          "each tagged with its source criterion.",
          "Recruiter edits, removes, adds or regenerates questions.",
          "System retains the guide for retrieval before the interview."])

uc_block(doc, "UC-4 — Monitor hiring pipeline and stale positions",
         "Recruiter (primary), System Scheduler (supporting)",
         "See the health of every open position at a glance, and be told when one is stalling.",
         ["System aggregates pipeline metrics across all open positions.",
          "Dashboard shows per-position funnels and days open against expected time-to-fill.",
          "Scheduler periodically evaluates every open position against its staleness rule.",
          "Breaching positions are flagged, an alert is published, and the responsible "
          "Recruiter is notified.",
          "The flag clears when the position is closed or the condition is resolved."])

uc_block(doc, "UC-5 — Enforce candidate data retention",
         "System Scheduler (primary), Admin (configures the policy)",
         "Make sure candidate data is not kept longer than the company is entitled to keep it — "
         "without anyone remembering to check.",
         ["Admin configures the retention period, the anchor date and the expiry action.",
          "Scheduler periodically evaluates every candidate record against the policy.",
          "Records entering the warning window are flagged and the Recruiter is notified with "
          "the expiry date.",
          "On expiry the system deletes or anonymises the record and everything derived from "
          "it, including stored resume files.",
          "System writes an audit entry and removes the candidate from the talent pool."])

uc_block(doc, "UC-6 — Manage members and roles", "Admin",
         "Control who has access and what each member may do.",
         ["Admin creates a member account, removes a member, or changes a member's role.",
          "For a new member, Admin sets email, an initial password and a role.",
          "System applies the change to the membership list.",
          "Admin passes the credentials to the new member outside the system."])

subhead(doc, "Deferred", before=12)
rich(doc, [
    ("D-1 — Re-match the talent pool against a job opening.", True, False),
    (" Score candidates the company has already seen against a new opening, instead of "
     "sourcing from zero. Deferred: it cannot be demonstrated without a deep talent pool, "
     "which this project will not have. It is a scope decision, not an omission — the "
     "retention and talent-pool requirements that make it possible are already in place.",
     False, False),
], size=9.5, after=4)

# ── Functional Requirements ─────────────────────────────────────────────────
doc.add_page_break()
heading(doc, "Functional Requirements", first=True)
fr_groups = parse_reqs(FR_SRC, "FR-", 2)
fr_count = sum(len(rows) for _, rows in fr_groups)
para(doc,
     f"{fr_count} functional requirements, each a single verifiable statement carrying no "
     "design or technology choice. Numbered FR-<use case>.<n> so every requirement traces to "
     "the use case it serves, and so adding one does not renumber the rest. IDs are stable "
     "once written and never reused; a withdrawn requirement leaves a gap.", after=8)
for title, rows in fr_groups:
    subhead(doc, title)
    table(doc, ["ID", "Requirement"], rows, [0.7, 6.0])

# ── Non-functional Requirements ─────────────────────────────────────────────
doc.add_page_break()
heading(doc, "Non-functional Requirements", first=True)
nfr_groups = parse_reqs(NFR_SRC, "NFR-", 3)
nfr_count = sum(len(rows) for _, rows in nfr_groups)
rich(doc, [
    (f"{nfr_count} non-functional requirements, grouped by the quality attribute each serves "
     "and paired with how it is verified. ", False, False),
    ("Scalability is the quality attribute this project demonstrates.", True, False),
], after=6)
rich(doc, [
    ("Every figure is an initial target, not a measured result — they exist to give the "
     "architecture direction, and will be revised once load testing produces real numbers. "
     "PDPA compliance is not stated as a single requirement because it is not verifiable as "
     "one; it is the combined effect of NFR-12 to NFR-14 and the UC-5 retention requirements.",
     False, True),
], size=9.5, after=8)
for title, rows in nfr_groups:
    subhead(doc, title)
    table(doc, ["ID", "Requirement", "Verified by"], rows, [0.7, 4.9, 1.1])

# ── Microservice Design with Collaborations ─────────────────────────────────
# Landscape: the collaborators column does not fit in portrait.
arch = doc.add_section(WD_SECTION.NEW_PAGE)
arch.orientation = WD_ORIENT.LANDSCAPE
arch.page_width, arch.page_height = arch.page_height, arch.page_width
arch.top_margin = arch.bottom_margin = Inches(0.6)
arch.left_margin = arch.right_margin = Inches(0.6)

heading(doc, "Microservice Design with Collaborations", first=True)
para(doc,
     "Five services behind one API gateway, with a message broker carrying the screening "
     "handover. The diagram shows who calls whom; the table below lists every operation each "
     "service exposes and every operation it calls on another. The two describe the same "
     "system and must agree. This is version 2 of the design: the screening handover between "
     "Hiring and Resume Processing has moved from REST onto RabbitMQ (ADR-013).", after=6)

diagram(doc, ARCH_DIAGRAM, 9.4)
para(doc,
     "An arrow A \u2192 B means \u201cA calls B\u201d; responses are not drawn. A service\u2019s "
     "private store is its own \u2014 no service reads another\u2019s, it calls the owning "
     "service instead. An adapter box stands for an external system the service reaches "
     "only through it. Green arrows are REST over HTTP/JSON, orange are gRPC (the two into "
     "the AI Service), and purple are RabbitMQ: Hiring publishes screening.requested, one per "
     "resume, and Resume Processing publishes screening.completed with the result.",
     size=8.5, italic=True, color=MUTED, align=WD_ALIGN_PARAGRAPH.CENTER, after=10)

subhead(doc, "Service \u2013 Operations \u2013 Collaborators", before=6)
soc_table(doc, parse_soc(SOC_SRC), [2.4, 2.9, 4.4])

para(doc, "", after=4)
para(doc,
     "The API Gateway is the entry point, not a domain service: it terminates TLS, routes, "
     "rate-limits, verifies the access token itself against the Identity Service\u2019s public "
     "key and resolves the caller\u2019s role from it, and records a rejected request to the "
     "access audit log. It is the only place a token is checked: the services behind it trust "
     "the deployment\u2019s private network (ADR-010). For the web application it keeps the refresh token in an HttpOnly "
     "cookie (ADR-012). It owns no data. "
     "External systems \u2014 object storage, the model provider, the email provider \u2014 are each "
     "reached only through an adapter. No service reads another service\u2019s data store; every "
     "crossing is one of the operations above or a message on one of the two screening queues.",
     size=9.5, after=0)

doc.save(OUT)
print("wrote", OUT)
