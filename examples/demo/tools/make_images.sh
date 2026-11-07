#!/usr/bin/env bash
# Draws the images and the scanned PDF of the demo case with ImageMagick and
# img2pdf. All names, places and numbers are invented.
#
# Usage: make_images.sh <out-dir>
set -euo pipefail

out=${1:?usage: make_images.sh <out-dir>}
mkdir -p "$out"
fonts=/System/Library/Fonts/Supplemental
serif="$fonts/Times New Roman.ttf"
sans="$fonts/Arial.ttf"
bold="$fonts/Arial Bold.ttf"
mono="$fonts/Courier New.ttf"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# scan makes a clean page look like a scan: gray, a small tilt, noise, blur.
scan() {
  magick "$1" -colorspace Gray -background white -rotate "$3" -gravity center -extent 1240x1754 \
    -attenuate 0.15 -seed 1 +noise Gaussian -level 5%,97% -strip -quality 80 "$2"
}

# Page 1 of the connection offer: a letter.
cat > "$tmp/letter.txt" <<'TXT'
27 October 2026
Our reference: EVN-48213

Ms Maya Collins
Flat 4B, Linden Court
12 Alder Street, Fairhaven FH1 4LD

Connection offer: electric vehicle charger, parking space 18

Dear Ms Collins,

Following the application made by Greenline EV Installs on 2 October 2026 and our site survey on 20 October 2026, we offer a new 32 A single-phase supply for an electric vehicle charger at parking space 18, level -1.

Point of connection: the meter room of Block A, level -1, next to the ramp. The supply cabinet in the Block C meter room is full and cannot take a new meter.

Connection charge: GBP 210.00, payable by your installer before the works.

Before we start the works, we need the written consent of the owner of the common parts, Linden Court Residents' Association, for the cable route from Block A to your parking space.

This offer is valid for 60 days, until 26 December 2026. To accept it, sign and return this page.

Yours sincerely,

Rachel Moore
Connections Officer
TXT
magick -size 1000x -background white -font "$serif" -pointsize 26 -interline-spacing 6 caption:@"$tmp/letter.txt" "$tmp/letter.png"
magick -size 1240x1754 xc:white \
  -font "$bold" -pointsize 40 -annotate +120+150 'EASTVALE NETWORKS' \
  -font "$sans" -pointsize 20 -annotate +120+185 'Connections Team - Eastvale House, 40 Quarry Road, Fairhaven FH2 7QT' \
  -stroke black -strokewidth 2 -draw 'line 120,205 1120,205' -stroke none \
  "$tmp/letter.png" -geometry +120+250 -composite \
  -font "$sans" -pointsize 16 -annotate +120+1690 'Eastvale Networks Ltd - Page 1 of 2' \
  "$tmp/p1.png"

# photo draws a simple room scene: wall, floor, boxes and a sign.
photo() { # out width height sign boxes-color
  local f=$1 w=$2 h=$3 sign=$4 color=$5
  magick -size "${w}x${h}" gradient:'#d9d4c7-#9c968a' \
    -fill '#6f6a60' -draw "rectangle 0,$((h*3/4)) $w,$h" \
    -fill "$color" -stroke '#333' -strokewidth 3 \
    -draw "rectangle $((w/10)),$((h/4)) $((w*3/10)),$((h*2/3))" \
    -draw "rectangle $((w*4/10)),$((h/4)) $((w*6/10)),$((h*2/3))" \
    -draw "rectangle $((w*7/10)),$((h/4)) $((w*9/10)),$((h*2/3))" -stroke none \
    -fill white -draw "rectangle $((w/8)),$((h/14)) $((w*7/8)),$((h/6))" \
    -fill black -font "$bold" -pointsize $((h/20)) -gravity north -annotate +0+$((h/13)) "$sign" \
    -gravity none -attenuate 0.6 -seed 1 +noise Gaussian -blur 0x1 "$f"
}

# Page 2 of the connection offer: the survey photos.
photo "$tmp/survey1.png" 900 480 'BLOCK A - METER ROOM' '#b8c4cc'
photo "$tmp/survey2.png" 900 480 'BLOCK C - SUPPLY CABINET' '#c9b18a'
magick -size 1240x1754 xc:white \
  -font "$bold" -pointsize 30 -annotate +120+150 'Annex A - Site survey photos (ref. EVN-48213)' \
  "$tmp/survey1.png" -geometry +170+220 -composite \
  -font "$sans" -pointsize 22 -annotate +170+740 'Photo 1: Block A meter room, level -1. Space for a new meter.' \
  "$tmp/survey2.png" -geometry +170+820 -composite \
  -annotate +170+1340 'Photo 2: Block C supply cabinet. No free ways.' \
  -annotate +170+1420 'Photos taken by K. Adams, Eastvale surveyor, on 20 October 2026.' \
  -font "$sans" -pointsize 16 -annotate +120+1690 'Eastvale Networks Ltd - Page 2 of 2' \
  "$tmp/p2.png"
scan "$tmp/p1.png" "$tmp/scan1.jpg" 0.6
scan "$tmp/p2.png" "$tmp/scan2.jpg" -0.4
img2pdf --nodate --engine internal --pagesize A4 -o "$out/connection-offer-EVN-48213.pdf" "$tmp/scan1.jpg" "$tmp/scan2.jpg"

# The cable route plan of the installer.
magick -size 1400x900 xc:white -stroke black -strokewidth 3 -fill none \
  -draw 'rectangle 40,40 1360,760' \
  -fill '#e8eef5' -draw 'rectangle 60,60 300,260' \
  -fill '#f5e8e8' -draw 'rectangle 620,60 780,260' \
  -fill '#e8f5ea' -draw 'rectangle 1100,60 1340,260' \
  -fill none -strokewidth 2 \
  -draw 'rectangle 380,480 480,720' -draw 'rectangle 480,480 580,720' -draw 'rectangle 580,480 680,720' \
  -draw 'rectangle 680,480 780,720' -draw 'rectangle 780,480 880,720' -draw 'rectangle 880,480 980,720' \
  -draw 'rectangle 980,480 1080,720' -draw 'rectangle 1080,480 1180,720' \
  -fill '#fff3b0' -draw 'rectangle 780,480 880,720' \
  -stroke '#1a8f3a' -strokewidth 6 -fill none -draw 'polyline 1220,260 1220,400 840,400 840,480' \
  -stroke '#c62828' -strokewidth 6 -draw 'polyline 300,230 820,230 820,480' \
  -stroke none -fill black -font "$bold" -pointsize 24 \
  -annotate +80+150 'BLOCK A' -annotate +80+185 'meter room' \
  -annotate +640+150 'BLOCK B' -annotate +640+185 'fire door' \
  -annotate +1120+150 'BLOCK C' -annotate +1120+185 'meter room' \
  -pointsize 26 -annotate +395+610 '14' -annotate +495+610 '15' -annotate +595+610 '16' -annotate +695+610 '17' \
  -annotate +795+610 '18' -annotate +895+610 '19' -annotate +995+610 '20' -annotate +1095+610 '21' \
  -font "$sans" -pointsize 22 -fill '#1a8f3a' -annotate +900+440 'Route C: 25 m (proposed)' \
  -fill '#c62828' -annotate +330+300 'Route A: 60 m, crosses the Block B fire door' \
  -fill black -font "$bold" -pointsize 22 -annotate +60+800 'Greenline EV Installs - Cable route plan - Linden Court, level -1' \
  -font "$sans" -pointsize 20 -annotate +60+835 'Drawing GL-117-01, 30/10/2026. Charger at parking space 18 (yellow). Not to scale.' \
  "$out/cable-route-plan.png"

# WhatsApp photos from the installer.
photo "$out/00000007-PHOTO-2026-09-24-17-42-10.jpg" 1200 900 'BLOCK C - METER ROOM - LEVEL -1' '#c9b18a'
magick -size 1200x900 gradient:'#bdbdbd-#7a7a7a' \
  -fill '#555' -draw 'polygon 0,900 1200,900 900,350 300,350' \
  -stroke white -strokewidth 10 -draw 'line 300,900 420,350' -draw 'line 900,900 780,350' -stroke none \
  -fill white -font "$bold" -pointsize 140 -annotate +520+780 '18' \
  -fill '#e0e0e0' -stroke '#333' -strokewidth 3 -draw 'rectangle 560,200 640,290' -stroke none \
  -attenuate 0.6 -seed 1 +noise Gaussian -blur 0x1 -quality 75 \
  "$out/00000008-PHOTO-2026-09-24-17-44-55.jpg"
magick "$out/00000007-PHOTO-2026-09-24-17-42-10.jpg" -quality 75 "$out/00000007-PHOTO-2026-09-24-17-42-10.jpg"

# Screenshot of the land register.
magick -size 1280x800 xc:'#f3f4f6' \
  -fill '#d1d5db' -draw 'rectangle 0,0 1280,48' \
  -fill white -draw 'roundrectangle 160,10 1120,38 8,8' \
  -fill '#374151' -font "$sans" -pointsize 16 -annotate +180+30 'https://register.example.org/title/FHV-552190' \
  -fill '#1e3a5f' -draw 'rectangle 0,48 1280,130' \
  -fill white -font "$bold" -pointsize 30 -annotate +40+100 'Fairhaven Land Register - Title search' \
  -fill white -draw 'rectangle 40,160 1240,760' \
  -fill '#111827' -font "$bold" -pointsize 22 -annotate +70+210 'Title number: FHV-552190' \
  -font "$sans" -pointsize 20 \
  -annotate +70+260 'Property: Flat 4B with parking space 18 (level -1)' \
  -annotate +70+300 'Building: Linden Court, 12 Alder Street, Fairhaven FH1 4LD' \
  -annotate +70+340 'Registered owner: Maya Collins, since 14 June 2019' \
  -annotate +70+380 'Tenure: leasehold, 999 years from 1998' \
  -annotate +70+440 'Common parts (garage level -1, meter rooms, ramp):' \
  -annotate +70+480 'held by Linden Court Residents'"'"' Association, title FHV-552100' \
  -fill '#6b7280' -pointsize 16 -annotate +70+720 'Extract printed 3 November 2026. This page is not an official copy.' \
  "$out/land-register-FHV-552190.png"

# Card terminal slip of the deposit.
magick -size 420x760 xc:'#fbfaf5' -fill black -font "$mono" -pointsize 22 -annotate +30+60 \
'  GREENLINE EV INSTALLS
 Unit 3, Mill Lane
 Fairhaven FH3 2BX

 CARD PAYMENT
 18/09/2026      14:32
 VISA   **** **** 4417

 Deposit, quote Q-2026-117

 AMOUNT        GBP 300.00

 APPROVED
 Receipt GL-0918

 Balance due on
 completion: GBP 1,150.00' \
  -rotate 1.5 -attenuate 0.4 -seed 1 +noise Gaussian -blur 0x0.6 -quality 80 "$out/receipt-GL-0918.jpg"

# Logo of the installer, inline in its emails.
magick -size 240x60 xc:'#1a8f3a' -fill white -font "$bold" -pointsize 28 -gravity center -annotate +0+0 'Greenline EV' "$out/greenline-logo.png"

# PNG files keep their write time; drop it, so a new run gives the same bytes.
magick mogrify -strip -define png:exclude-chunk=date,time "$out"/*.png
