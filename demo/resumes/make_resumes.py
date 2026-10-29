"""Builds the three demo resumes as PDFs with a text layer.

Every person, company, school and contact detail here is invented. Never put a
real resume in this folder (PDPA).

    uv run --with reportlab demo/resumes/make_resumes.py
"""

from pathlib import Path

from reportlab.lib.pagesizes import A4
from reportlab.lib.styles import ParagraphStyle, getSampleStyleSheet
from reportlab.lib.units import mm
from reportlab.platypus import ListFlowable, ListItem, Paragraph, SimpleDocTemplate, Spacer

OUT = Path(__file__).parent

RESUMES = {
    # Meets both must-haves with room to spare, and every nice-to-have.
    "1-strong-match-ananda.pdf": {
        "name": "Ananda Sukprasert",
        "headline": "Senior Backend Engineer — Go, PostgreSQL, payments, DevOps",
        "contact": "ananda.s@example.com · +66 00 000 0001 · Bangkok",
        "summary": (
            "Backend engineer with seven years of production Go, six of them building payment "
            "systems. I own what I build from code to production — pipelines, infrastructure and "
            "on-call — and I like teaching others to do the same."
        ),
        "experience": [
            (
                "Senior Backend Engineer, Example Pay Co., Ltd.",
                "2021 – present",
                [
                    "Lead a team of four building the card-payment authorisation service in Go, "
                    "handling 1,200 transactions per second at peak.",
                    "Designed the PostgreSQL ledger schema (double-entry, 40 million rows a month) "
                    "and its partitioning; cut month-end reconciliation from six hours to twenty "
                    "minutes.",
                    "Moved settlement events onto Kafka with idempotent consumers, removing a "
                    "class of duplicate-payout incidents.",
                    "Built the team's DevOps platform: GitHub Actions pipelines that test, scan and "
                    "deploy every service to Kubernetes with Argo CD, about 30 releases a week.",
                    "Manage the AWS infrastructure as code with Terraform (EKS, RDS for PostgreSQL, "
                    "MSK), reviewed and applied through pull requests.",
                    "Set up monitoring and alerting with Prometheus, Grafana and PagerDuty; lead the "
                    "on-call rotation and its incident reviews.",
                ],
            ),
            (
                "Backend Engineer, Sample Wallet Ltd.",
                "2018 – 2021",
                [
                    "Built the top-up and transfer APIs in Go on PostgreSQL for a mobile wallet "
                    "with 2 million users.",
                    "Introduced RabbitMQ for notification fan-out and wrote the retry and "
                    "dead-letter handling.",
                    "Mentored three junior engineers through their first production releases.",
                ],
            ),
        ],
        "skills": "Go (7 years), PostgreSQL, Kafka, RabbitMQ, Docker, Kubernetes, Terraform, "
        "GitHub Actions, Argo CD, Prometheus, Grafana, AWS, gRPC, payment systems, PCI DSS",
        "education": "B.Eng. Computer Engineering, Example Institute of Technology, 2017",
    },
    # Meets both must-haves, only just, and some of the nice-to-haves: Docker but not
    # Kubernetes, a little RabbitMQ, a basic CI pipeline but no infrastructure as code or
    # monitoring, no payments. The score comes from the nice-to-haves alone (must-haves only
    # gate), so this lands in the middle.
    "2-partial-match-kanya.pdf": {
        "name": "Kanya Wongsiri",
        "headline": "Backend Developer — Go and PostgreSQL",
        "contact": "kanya.w@example.com · +66 00 000 0002 · Chiang Mai",
        "summary": (
            "Backend developer with three years of Go, building internal tools and APIs for a "
            "retail company. Comfortable with PostgreSQL; looking to work on larger systems."
        ),
        "experience": [
            (
                "Backend Developer, Demo Retail Group",
                "2023 – present",
                [
                    "Maintain the stock and pricing APIs, written in Go, used by 40 stores.",
                    "Write PostgreSQL queries and migrations for the inventory database.",
                    "Packaged the services as Docker images with Docker Compose for local "
                    "development; production still runs on a single virtual machine.",
                    "Set up a basic GitHub Actions pipeline that runs the tests on every pull "
                    "request; deploying is still a manual shell script, with no monitoring "
                    "beyond the server's logs.",
                    "Added a small RabbitMQ queue so price updates reach the stores without "
                    "polling; no Kubernetes or larger messaging experience yet.",
                ],
            ),
            (
                "Junior Developer, Placeholder Studio",
                "2022 – 2023",
                [
                    "Wrote Go services for a booking website and fixed bugs in its PHP front end.",
                    "Added PostgreSQL indexes that halved the search page's load time.",
                ],
            ),
        ],
        "skills": "Go (3 years), PostgreSQL, Docker, Docker Compose, RabbitMQ (basic), "
        "GitHub Actions (basic), REST APIs, Git, Linux, PHP",
        "education": "B.Sc. Information Technology, Example Northern University, 2022",
    },
    # Meets no must-have: a designer, not a backend engineer.
    "3-no-match-pimchanok.pdf": {
        "name": "Pimchanok Rattanakul",
        "headline": "UX/UI Designer",
        "contact": "pimchanok.r@example.com · +66 00 000 0003 · Bangkok",
        "summary": (
            "Product designer with five years of experience designing mobile apps and marketing "
            "websites. I turn research into clear interfaces and work closely with front-end "
            "developers."
        ),
        "experience": [
            (
                "Senior UX/UI Designer, Example Travel Co.",
                "2022 – present",
                [
                    "Redesigned the hotel booking flow in Figma; conversion rose 18% after launch.",
                    "Run usability tests with 10 users every sprint and turn them into design "
                    "changes.",
                    "Maintain the company design system of 120 components.",
                ],
            ),
            (
                "Graphic Designer, Sample Agency",
                "2020 – 2022",
                [
                    "Designed campaign visuals, social media content and landing pages for "
                    "consumer brands.",
                ],
            ),
        ],
        "skills": "Figma, Adobe XD, Illustrator, Photoshop, user research, prototyping, "
        "HTML and CSS basics",
        "education": "B.F.A. Communication Design, Example Arts University, 2019",
    },
}


def build(path: Path, r: dict) -> None:
    styles = getSampleStyleSheet()
    body = ParagraphStyle("body", parent=styles["BodyText"], fontSize=10, leading=14)
    small = ParagraphStyle("small", parent=body, fontSize=9, textColor="#555555")
    section = ParagraphStyle("section", parent=styles["Heading3"], spaceBefore=10, spaceAfter=4)

    story = [
        Paragraph(r["name"], styles["Title"]),
        Paragraph(r["headline"], styles["Heading4"]),
        Paragraph(r["contact"], small),
        Spacer(1, 6),
        Paragraph("Summary", section),
        Paragraph(r["summary"], body),
        Paragraph("Experience", section),
    ]
    for title, period, bullets in r["experience"]:
        story.append(Paragraph(f"<b>{title}</b> — {period}", body))
        story.append(
            ListFlowable(
                [ListItem(Paragraph(b, body), leftIndent=12) for b in bullets],
                bulletType="bullet",
                start="•",
                leftIndent=12,
            )
        )
        story.append(Spacer(1, 4))
    story += [
        Paragraph("Skills", section),
        Paragraph(r["skills"], body),
        Paragraph("Education", section),
        Paragraph(r["education"], body),
    ]

    doc = SimpleDocTemplate(
        str(path),
        pagesize=A4,
        leftMargin=18 * mm,
        rightMargin=18 * mm,
        topMargin=16 * mm,
        bottomMargin=16 * mm,
        title=f"{r['name']} — resume",
        author="HireAssist demo (synthetic)",
        subject="Synthetic resume for demonstrating HireAssist; the person is fictional.",
    )
    doc.build(story)


if __name__ == "__main__":
    for name, resume in RESUMES.items():
        build(OUT / name, resume)
        print("wrote", OUT / name)
