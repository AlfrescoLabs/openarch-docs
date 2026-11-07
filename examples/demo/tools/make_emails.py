"""Writes the emails of the demo case from the images of make_images.sh.

The emails copy the shapes of real mail: Gmail "Forwarded message" blocks,
quoted replies, a forward as attachment (message/rfc822), an HTML part with
an inline logo, and ISO-8859-1 with quoted-printable. All names, places and
numbers are invented.

Usage: python3 -I make_emails.py <assets-dir> <case-dir>
"""

import sys
from email import policy
from email.message import EmailMessage
from pathlib import Path

MAYA = "Maya Collins <maya.collins@example.com>"
TOM = "Tom Hughes <tom.hughes@example.net>"
EASTVALE = "Eastvale Networks Connections <connections@example.org>"
PRIYA = "Priya Shah <manager@lindencourt.example.org>"


def new(sender, to, subject, date, msgid, reply_to=None):
    m = EmailMessage(policy=policy.default)
    m["From"] = sender
    m["To"] = to
    m["Subject"] = subject
    m["Date"] = date
    m["Message-ID"] = f"<{msgid}@mail.example.com>"
    if reply_to:
        m["In-Reply-To"] = f"<{reply_to}@mail.example.com>"
        m["References"] = f"<{reply_to}@mail.example.com>"
    m["MIME-Version"] = "1.0"
    return m


def attach(m, path, maintype, subtype):
    m.add_attachment(path.read_bytes(), maintype=maintype, subtype=subtype, filename=path.name)


def fixed_boundaries(m, name):
    """Gives every multipart a fixed boundary, so a new run gives the same file."""
    for i, part in enumerate(m.walk()):
        if part.is_multipart():
            part.set_boundary(f"=_{name}_{i}")


def quote(text, depth=1):
    return "\n".join((">" * depth + " " + line).rstrip() for line in text.strip("\n").split("\n"))


def offer(assets):
    m = new(MAYA, TOM, "Fwd: Connection offer EVN-48213", "Thu, 29 Oct 2026 08:40:12 +0000", "fwd-offer")
    m.set_content("""Hi Tom,

Eastvale sent this on Tuesday. They want the connection at the Block A meter
room, not at Block C. Can you check the cable route and the cost?

Maya

---------- Forwarded message ---------
From: Eastvale Networks Connections <connections@example.org>
Date: Tue, 27 Oct 2026 at 15:05
Subject: Connection offer EVN-48213
To: Maya Collins <maya.collins@example.com>


Dear Ms Collins,

Please find attached our connection offer for the electric vehicle charger
at parking space 18, Linden Court. Please read the conditions on page 1
before you accept it.

Kind regards,

Rachel Moore
Connections Officer
Eastvale Networks
""")
    attach(m, assets / "connection-offer-EVN-48213.pdf", "application", "pdf")
    return "Fwd_ Connection offer EVN-48213.eml", m


def plan(assets):
    m = new(TOM, MAYA, "Cable route for space 18", "Fri, 30 Oct 2026 17:20:45 +0000", "plan")
    m.set_content("""Hi Maya,

I have looked at the Eastvale offer. Attached is the cable route plan
(drawing GL-117-01).

Route C, from the Block C meter room, is 25 m. This is the route of my quote.
Route A, from the Block A meter room that Eastvale wants, is 60 m and goes
through the Block B fire door. It needs a fire-rated sleeve at the door and
more cable: about £380 more than the quote.

I will ask Eastvale if they can take the supply from Block C after all.

Best regards,
Tom Hughes
Greenline EV Installs
""")
    attach(m, assets / "cable-route-plan.png", "image", "png")
    return "Cable route for space 18.eml", m


def photos_reply(assets):
    plan_text = """Hi Maya,

I have looked at the Eastvale offer. Attached is the cable route plan
(drawing GL-117-01).
[...]"""
    maya = """Hi Tom,

Did you take the photos on page 2 of the offer? The committee will ask who
took them and when.

Maya"""
    tom = """Hi Maya,

No, I did not take the photos on page 2 of the offer. The Eastvale surveyor,
K. Adams, took them during his visit on 20 October. I was on another job
that day.

I have asked Eastvale if the Block C cabinet can be upgraded for one more
meter. I will tell you as soon as they answer.

Best regards,
Tom Hughes
Greenline EV Installs"""
    plain = (tom + "\n\nOn Wed, 4 Nov 2026 at 08:47, Maya Collins <maya.collins@example.com> wrote:\n"
             + quote(maya) + "\n>\n> On Fri, 30 Oct 2026 at 17:20, Tom Hughes <tom.hughes@example.net> wrote:\n"
             + quote(plan_text, 2) + "\n")
    m = new(TOM, MAYA, "Re: Cable route for space 18", "Wed, 04 Nov 2026 10:05:31 +0000", "photos", "photos-q")
    m.set_content(plain, charset="iso-8859-1", cte="quoted-printable")
    html = "".join(f"<p>{p.replace(chr(10), '<br>')}</p>" for p in tom.split("\n\n"))
    m.add_alternative(f'<html><body>{html}<p><img src="cid:logo@greenline" alt="Greenline EV"></p></body></html>',
                      subtype="html", charset="iso-8859-1", cte="quoted-printable")
    m.get_payload()[1].add_related((assets / "greenline-logo.png").read_bytes(), "image", "png", cid="<logo@greenline>",
                                   filename="greenline-logo.png", disposition="inline")
    return "Re_ Cable route for space 18.eml", m


def receipt(assets):
    inner = new(TOM, MAYA, "Payment receipt - quote Q-2026-117", "Fri, 18 Sep 2026 14:40:02 +0100", "receipt")
    text = """Dear Maya,

Thank you for your deposit of £300.00 for quote Q-2026-117: a 7 kW wall
charger at parking space 18, Linden Court, with a cable from the Block C
meter room. The total price is £1,450.00 including VAT. The balance of
£1,150.00 is due on completion.

The card receipt is attached.

Best regards,
Tom Hughes
Greenline EV Installs"""
    inner.set_content(text)
    html = "".join(f"<p>{p.replace(chr(10), '<br>')}</p>" for p in text.split("\n\n"))
    inner.add_alternative(f'<html><body><p><img src="cid:logo@greenline" alt="Greenline EV"></p>{html}</body></html>',
                          subtype="html")
    inner.get_payload()[1].add_related((assets / "greenline-logo.png").read_bytes(), "image", "png",
                                       cid="<logo@greenline>", filename="greenline-logo.png", disposition="inline")
    attach(inner, assets / "receipt-GL-0918.jpg", "image", "jpeg")

    m = new(MAYA, MAYA, "Fwd: Payment receipt - quote Q-2026-117", "Tue, 03 Nov 2026 18:55:20 +0000", "fwd-receipt")
    m.set_content("For the file.\n")
    m.add_attachment(inner, filename="Payment receipt - quote Q-2026-117.eml")
    return "Fwd_ Payment receipt - quote Q-2026-117.eml", m


NOTICE = """Dear Priya,

I give written notice that I want to install an electric vehicle charger at
my parking space 18 (level -1), at my own cost, under rule 4.1 of the garage
rules.

Eastvale Networks wants the connection at the Block A meter room (offer
EVN-48213). My installer, Greenline EV Installs, proposes a shorter cable
route from the Block C meter room. I will send you both documents.

I attach the land register extract for my flat and parking space.

Kind regards,
Maya Collins
Flat 4B"""

CONDITIONS = """Dear Maya,

Thank you for your notice of 3 November. The committee will look at it at
its meeting on 12 November 2026.

Please send us before then the Eastvale offer letter and the cable plan of
your installer. One condition is already clear: no cable may pass through
the Block B fire door.

Kind regards,
Priya Shah
Building Manager
Linden Court Residents' Association"""


def notice(assets):
    m = new(MAYA, PRIYA, "Charger at space 18 - notice", "Tue, 03 Nov 2026 19:10:05 +0000", "notice")
    m.set_content(NOTICE + "\n")
    attach(m, assets / "land-register-FHV-552190.png", "image", "png")
    return "Charger at space 18 - notice.eml", m


def conditions(assets):
    m = new(PRIYA, MAYA, "Re: Charger at space 18 - notice", "Thu, 05 Nov 2026 16:30:44 +0000", "conditions", "notice")
    m.set_content(CONDITIONS + "\n\nOn Tue, 3 Nov 2026 at 19:10, Maya Collins <maya.collins@example.com> wrote:\n"
                  + quote(NOTICE) + "\n")
    return "Re_ Charger at space 18 - notice.eml", m


def forward_conditions(assets):
    m = new(MAYA, TOM, "Fwd: Re: Charger at space 18 - notice", "Fri, 06 Nov 2026 09:02:17 +0000", "fwd-conditions")
    m.set_content("""See below.

---------- Forwarded message ---------
From: Priya Shah <manager@lindencourt.example.org>
Date: Thu, 5 Nov 2026 at 16:30
Subject: Re: Charger at space 18 - notice
To: Maya Collins <maya.collins@example.com>


""" + CONDITIONS + "\n")
    return "Fwd_ Re_ Charger at space 18 - notice.eml", m


def main():
    assets, case = Path(sys.argv[1]), Path(sys.argv[2])
    case.mkdir(parents=True, exist_ok=True)
    for make in (offer, plan, photos_reply, receipt, notice, conditions, forward_conditions):
        name, m = make(assets)
        fixed_boundaries(m, Path(name).stem.replace(" ", "_"))
        (case / name).write_bytes(m.as_bytes(policy=policy.default))


if __name__ == "__main__":
    main()
