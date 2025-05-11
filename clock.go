package main

import (
	"fmt"
	"net"
	"time"

	"github.com/racingmars/go3270"
)

// Simple session type for the clock functionality
type ClockSession struct {
	username string
}

// getCenteredPosition calculates the column position to center text
func getCenteredPosition(text string, screenWidth int) int {
	return (screenWidth - len(text)) / 2
}

// ASCII Art digits for the clock - shorter and narrower version
var bigDigits = [][]string{
	{ // 0
		" 000000 ",
		"00    00",
		"00    00",
		"00    00",
		"00    00",
		"00    00",
		"00    00",
		"00    00",
		" 000000 ",
	},
	{ // 1
		"   11   ",
		"  111   ",
		" 1111   ",
		"   11   ",
		"   11   ",
		"   11   ",
		"   11   ",
		"   11   ",
		" 111111 ",
	},
	{ // 2
		" 222222 ",
		"22    22",
		"     22 ",
		"    22  ",
		"   22   ",
		"  22    ",
		" 22     ",
		"22      ",
		"22222222",
	},
	{ // 3
		" 333333 ",
		"33    33",
		"      33",
		"     33 ",
		"  3333  ",
		"     33 ",
		"      33",
		"33    33",
		" 333333 ",
	},
	{ // 4
		"    44  ",
		"   444  ",
		"  4444  ",
		" 44 44  ",
		"44  44  ",
		"44444444",
		"    44  ",
		"    44  ",
		"    44  ",
	},
	{ // 5
		"5555555 ",
		"55      ",
		"55      ",
		"555555  ",
		"     55 ",
		"      55",
		"      55",
		"55    55",
		" 555555 ",
	},
	{ // 6
		" 666666 ",
		"66    66",
		"66      ",
		"666666  ",
		"66    66",
		"66    66",
		"66    66",
		"66    66",
		" 666666 ",
	},
	{ // 7
		"7777777 ",
		"     77 ",
		"    77  ",
		"   77   ",
		"  77    ",
		" 77     ",
		"77      ",
		"77      ",
		"77      ",
	},
	{ // 8
		" 888888 ",
		"88    88",
		"88    88",
		"88    88",
		" 888888 ",
		"88    88",
		"88    88",
		"88    88",
		" 888888 ",
	},
	{ // 9
		" 999999 ",
		"99    99",
		"99    99",
		"99    99",
		" 9999999",
		"      99",
		"      99",
		"99    99",
		" 999999 ",
	},
}

// Colon separator for the clock - narrower version
var bigColon = []string{
	" ",
	" ",
	":",
	":",
	" ",
	" ",
	":",
	":",
	" ",
}

// Refresh interval for the clock (1.2 seconds)
const clockRefreshInterval = 1200 * time.Millisecond

// Timezone names for display and cycling
var timezoneNames = []string{
	"UTC",
	"New York",
	"London",
	"Rome",
	"Tokyo",
}

// Location objects for the different timezones
var timezoneLocations = []string{
	"UTC",
	"America/New_York",
	"Europe/London",
	"Europe/Rome",
	"Asia/Tokyo",
}

// ASCII Art IBM logo for display at the top of each hour
var ibmLogo = []string{
	"IIIIIIIIIII  BBBBBBBBBBBB      MMMMMMMM      MMMMMMMM",
	"IIIIIIIIIII  BBBBBBBBBBBBBBB   MMMMMMMMM    MMMMMMMMM",
	"   IIIII        BBBB   BBBBB     MMMMMMMM  MMMMMMMM",
	"   IIIII        BBBBBBBBBBB      MMMM  MMM MMM MMMM",
	"   IIIII        BBBBBBBBBBB      MMMM  MMMMMMM MMMM",
	"   IIIII        BBBB   BBBBB     MMMM   MMMMM  MMMM",
	"IIIIIIIIIII  BBBBBBBBBBBBBBB   MMMMMM    MMM   MMMMMM",
	"IIIIIIIIIII  BBBBBBBBBBBB      MMMMMM     M    MMMMMM",
}

// Function to draw a big clock screen
func ShowClock(conn net.Conn, username string) error {
	// Keep track of logo test mode and timezone
	showLogoTest := false
	currentTimezone := 0

	// Function to create a fresh screen with the latest time
	createScreen := func() go3270.Screen {
		// Get current time in the selected timezone
		now := time.Now().UTC() // Start with UTC

		// Apply the selected timezone
		if currentTimezone > 0 && currentTimezone < len(timezoneLocations) {
			loc, err := time.LoadLocation(timezoneLocations[currentTimezone])
			if err == nil {
				now = time.Now().In(loc)
			}
		}

		// Format time for display
		currentTime := now.Format("15:04:05")

		// Create screen
		screen := go3270.Screen{}

		// Add timezone indicator and username at the top (centered)
		tzName := timezoneNames[currentTimezone]
		tzTitle := fmt.Sprintf("Secure3270Proxy Clock - User: %s - Timezone: %s", username, tzName)
		screen = append(screen, go3270.Field{
			Row:     0,
			Col:     getCenteredPosition(tzTitle, 79),
			Content: tzTitle,
			Color:   go3270.Turquoise,
			Intense: true,
		})

		// Calculate position to center the clock
		// Each digit is 8 chars wide, colon is 1 char wide, total is 8*6 + 1*2 = 50 for HH:MM:SS
		clockWidth := 50
		startCol := (79-clockWidth)/2 - 7 // Shift 7 columns to the left

		// Draw the big clock - starts at row 1
		startRow := 1

		// Determine color based on current minute
		// Cycle through: White -> Green -> Blue -> Red -> Turquoise -> Pink -> Yellow -> White...
		var digitColor go3270.Color
		minute := now.Minute()
		switch minute % 7 {
		case 0:
			digitColor = go3270.White
		case 1:
			digitColor = go3270.Green
		case 2:
			digitColor = go3270.Blue
		case 3:
			digitColor = go3270.Red
		case 4:
			digitColor = go3270.Turquoise
		case 5:
			digitColor = go3270.Pink
		case 6:
			digitColor = go3270.Yellow
		}

		// Check if we should show the IBM logo (top of hour or test mode)
		isTopOfHour := now.Minute() == 0 && now.Second() < 30
		showLogo := isTopOfHour || showLogoTest

		if showLogo {
			// Display IBM logo instead of time digits
			logoCol := (79 - len(ibmLogo[0])) / 2 // Center the logo horizontally
			for i, line := range ibmLogo {
				screen = append(screen, go3270.Field{
					Row:     startRow + i + 1, // Position logo with a bit of spacing
					Col:     logoCol,
					Content: line,
					Color:   go3270.Blue, // IBM Blue!
					Intense: true,
				})
			}

			// If in test mode, show an indicator
			if showLogoTest && !isTopOfHour {
				screen = append(screen, go3270.Field{
					Row:     20,
					Col:     15,
					Content: "Logo test mode (Press F12 again to exit test mode)",
					Color:   go3270.Blue,
					Intense: true,
				})
			}
		} else {
			// Extract individual digits and colons from the time string
			for i, ch := range currentTime {
				col := startCol
				if i > 0 {
					// Adjust column based on preceding characters
					// Each digit is 8 chars wide, colon is 1 char wide
					if i <= 2 { // H:
						col = startCol + i*8 + (i/2)*1
					} else if i <= 5 { // H:M:
						col = startCol + i*8 + ((i-1)/2)*1
					} else { // H:M:S
						col = startCol + i*8 + ((i-2)/2)*1
					}
				}

				if ch == ':' {
					// Draw colon - colons remain white for better contrast
					for row, line := range bigColon {
						screen = append(screen, go3270.Field{
							Row:     startRow + row,
							Col:     col,
							Content: line,
							Color:   go3270.White,
						})
					}
				} else {
					// Convert char to digit index
					digit := int(ch - '0')
					if digit >= 0 && digit <= 9 {
						// Draw the digit with the color based on minute
						for row, line := range bigDigits[digit] {
							screen = append(screen, go3270.Field{
								Row:     startRow + row,
								Col:     col,
								Content: line,
								Color:   digitColor,
								Intense: true,
							})
						}
					}
				}
			}
		}

		// Add world time information below the clock or logo
		// Add world times - split into two rows for better fit
		// Get times for different cities
		nyLocation, _ := time.LoadLocation("America/New_York")
		londonLocation, _ := time.LoadLocation("Europe/London")
		romeLocation, _ := time.LoadLocation("Europe/Rome")
		tokyoLocation, _ := time.LoadLocation("Asia/Tokyo")

		nyTime := time.Now().In(nyLocation).Format("15:04")
		londonTime := time.Now().In(londonLocation).Format("15:04")
		romeTime := time.Now().In(romeLocation).Format("15:04")
		tokyoTime := time.Now().In(tokyoLocation).Format("15:04")

		worldTimeStr1 := fmt.Sprintf("NY: %s  London: %s", nyTime, londonTime)
		worldTimeStr2 := fmt.Sprintf("Rome: %s  Tokyo: %s", romeTime, tokyoTime)

		var worldTimeRow int
		if showLogo {
			worldTimeRow = startRow + len(ibmLogo) + 2 // After the IBM logo with spacing
		} else {
			worldTimeRow = startRow + 11 // After the clock digits (9 rows tall)
		}

		screen = append(screen, go3270.Field{
			Row:     worldTimeRow,
			Col:     getCenteredPosition(worldTimeStr1, 79),
			Content: worldTimeStr1,
			Color:   go3270.Green,
		})

		screen = append(screen, go3270.Field{
			Row:     worldTimeRow + 1,
			Col:     getCenteredPosition(worldTimeStr2, 79),
			Content: worldTimeStr2,
			Color:   go3270.Green,
		})

		// Add date at the bottom
		dateFormat := now.Format("Monday, January 2, 2006")
		dateStr := fmt.Sprintf("Date: %s", dateFormat)
		screen = append(screen, go3270.Field{
			Row:     worldTimeRow + 3,
			Col:     getCenteredPosition(dateStr, 79),
			Content: dateStr,
			Color:   go3270.Turquoise,
		})

		// Add function key legends at the bottom
		screen = append(screen, go3270.Field{
			Row:     22,
			Col:     2,
			Content: "F3=Return to Host Menu",
			Color:   go3270.Blue,
		})

		screen = append(screen, go3270.Field{
			Row:     22,
			Col:     25,
			Content: "F11=Cycle Timezone",
			Color:   go3270.Blue,
		})

		screen = append(screen, go3270.Field{
			Row:     22,
			Col:     45,
			Content: "F12=Display IBM Logo",
			Color:   go3270.Blue,
		})

		return screen
	}

	// Function to update the screen without waiting for input
	updateScreenNoWait := func() error {
		screen := createScreen()

		// Show the screen but don't wait for a response
		_, err := go3270.ShowScreenOpts(screen, nil, conn,
			go3270.ScreenOpts{
				CursorRow:  22,
				CursorCol:  40,
				NoResponse: true,
			})
		return err
	}

	// Get input with a timeout for auto-refresh
	getInputWithTimeout := func(timeoutMs int) (go3270.Response, error, bool) {
		screen := createScreen()

		// Set a timeout on the connection to implement non-blocking IO
		conn.SetReadDeadline(time.Now().Add(time.Millisecond * time.Duration(timeoutMs)))

		// Show screen and try to get input (might timeout)
		response, err := go3270.ShowScreenOpts(screen, nil, conn,
			go3270.ScreenOpts{
				CursorRow:  22,
				CursorCol:  40,
				NoResponse: false,
			})

		// Check if this was a timeout
		timeout := false
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			timeout = true
			err = nil // Timeout isn't a real error in this case
		}

		// Reset deadline to "no deadline"
		conn.SetReadDeadline(time.Time{})

		return response, err, timeout
	}

	// Initial screen update
	if err := updateScreenNoWait(); err != nil {
		return fmt.Errorf("error showing initial clock screen: %v", err)
	}

	// Main clock loop
	lastRefreshTime := time.Now()
	timeoutMs := int(clockRefreshInterval / time.Millisecond)

	// Skip immediate refresh on first loop to avoid double refresh
	skipImmediate := true

	for {
		// Try to get input with timeout
		response, err, timeout := getInputWithTimeout(timeoutMs)
		if err != nil {
			return fmt.Errorf("error getting input: %v", err)
		}

		// If we got user input, process it
		if !timeout {
			switch response.AID {
			case go3270.AIDPF3:
				// Return to main menu
				return nil

			case go3270.AIDPF11:
				// Cycle to the next timezone
				currentTimezone = (currentTimezone + 1) % len(timezoneNames)
				// Reset refresh timer
				lastRefreshTime = time.Now()
				// Update screen immediately
				if err := updateScreenNoWait(); err != nil {
					return fmt.Errorf("error updating clock after F11: %v", err)
				}
				continue

			case go3270.AIDPF12:
				// Toggle logo test mode
				showLogoTest = !showLogoTest
				// Reset refresh timer
				lastRefreshTime = time.Now()
				// Update screen immediately
				if err := updateScreenNoWait(); err != nil {
					return fmt.Errorf("error updating clock after F12: %v", err)
				}
				continue
			}
		}

		// Check if it's time to refresh the clock
		elapsedTime := time.Since(lastRefreshTime)

		// Only refresh if enough time has passed and we're not skipping the immediate refresh
		if (timeout || elapsedTime >= clockRefreshInterval) && !skipImmediate {
			// Reset refresh time BEFORE updating to avoid time drift
			lastRefreshTime = time.Now()

			// Update the clock display
			if err := updateScreenNoWait(); err != nil {
				return fmt.Errorf("error updating clock: %v", err)
			}
		}

		// Clear the skip flag after first iteration
		if skipImmediate {
			skipImmediate = false
		}
	}
}

// ShowClockWithLogo shows the clock screen with the IBM logo already displayed
func ShowClockWithLogo(conn net.Conn, username string) error {
	// Function to create a screen with the IBM logo displayed
	createScreen := func() go3270.Screen {
		// Create screen
		screen := go3270.Screen{}

		// Add title at the top (centered)
		tzTitle := fmt.Sprintf("Secure3270Proxy - IBM Logo - User: %s", username)
		screen = append(screen, go3270.Field{
			Row:     0,
			Col:     getCenteredPosition(tzTitle, 79),
			Content: tzTitle,
			Color:   go3270.Turquoise,
			Intense: true,
		})

		// Display IBM logo
		logoCol := (79 - len(ibmLogo[0])) / 2 // Center the logo horizontally
		for i, line := range ibmLogo {
			screen = append(screen, go3270.Field{
				Row:     5 + i, // Position logo in the middle of screen
				Col:     logoCol,
				Content: line,
				Color:   go3270.Blue, // IBM Blue!
				Intense: true,
			})
		}

		// Add key hint at bottom
		screen = append(screen, go3270.Field{
			Row:     22,
			Col:     2,
			Content: "Press F3 to return to Host Menu",
			Color:   go3270.Blue,
		})

		return screen
	}

	// Show the IBM logo screen
	screen := createScreen()
	response, err := go3270.ShowScreen(screen, nil, 22, 2, conn)
	if err != nil {
		return fmt.Errorf("error showing IBM logo: %v", err)
	}

	// Only return to host menu when F3 is pressed
	if response.AID == go3270.AIDPF3 {
		return nil
	}

	// Otherwise, show the regular clock screen with logo mode enabled
	return ShowClock(conn, username)
}
