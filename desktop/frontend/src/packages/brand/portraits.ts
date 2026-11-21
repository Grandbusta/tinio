// The six ready characters as 12 by 12 pixel portraits, each in their own colours.
// Keys of a map are looked up in its palette; "." is the background tint.
export const portraits = {
  "zara": {
    "name": "Zara",
    "map": [
      "...hhhhhh...",
      "..hhhhhhhh..",
      ".hhhhhhhhhh.",
      ".hhsssssshh.",
      "..asessesa..",
      "..ssssssss..",
      "..sssmmsss..",
      "...ssssss...",
      ".....ss.....",
      "...cccccc...",
      "..cccccccc..",
      ".cccccccccc."
    ],
    "palette": {
      ".": "#FDE8EF",
      "h": "#15100D",
      "s": "#5C3A24",
      "e": "#15100D",
      "m": "#3A2015",
      "c": "#D6457A",
      "a": "#F2C94C"
    }
  },
  "kenji": {
    "name": "Kenji",
    "map": [
      "............",
      "...hhhhhh...",
      "..hhhhhhhh..",
      "..hssssssh..",
      "..gegssgeg..",
      "..ssssssss..",
      "..sssmmsss..",
      "...ssssss...",
      ".....ss.....",
      "...caccac...",
      "..cccccccc..",
      ".cccccccccc."
    ],
    "palette": {
      ".": "#E3F0FA",
      "h": "#111111",
      "s": "#EBC9A8",
      "e": "#1B1B1B",
      "m": "#B0705A",
      "g": "#3A3F4B",
      "c": "#4A5568",
      "a": "#CBD5E1"
    }
  },
  "marcus": {
    "name": "Marcus",
    "map": [
      "............",
      "...hhhhhh...",
      "..hhhhhhhh..",
      "..ssssssss..",
      "..ssessess..",
      "..ssssssss..",
      "..sbbmmbbs..",
      "...bbbbbb...",
      ".....ss.....",
      "...cwttwc...",
      "..ccwttwcc..",
      ".cccctttccc."
    ],
    "palette": {
      ".": "#E8EDF3",
      "h": "#121212",
      "s": "#4E3020",
      "e": "#121212",
      "m": "#2A1810",
      "b": "#1E1410",
      "c": "#1F2A44",
      "w": "#FFFFFF",
      "t": "#B23A3A"
    }
  },
  "sofia": {
    "name": "Sofía",
    "map": [
      "...hhhhhh...",
      "..hhhhhhhh..",
      ".hhhhhhhhhh.",
      ".hhsssssshh.",
      ".hhsesseshh.",
      ".hhsssssshh.",
      ".hhssmmsshh.",
      ".hhhsssshhh.",
      ".hhh.ss.hhh.",
      ".hhcccccchh.",
      "..cccccccc..",
      ".cccccccccc."
    ],
    "palette": {
      ".": "#E2F3F3",
      "h": "#3A2418",
      "s": "#D29E78",
      "e": "#1B1B1B",
      "m": "#A8483E",
      "c": "#1F7F86"
    }
  },
  "henrik": {
    "name": "Henrik",
    "map": [
      "............",
      "...hhhhhh...",
      "..hhhhhhhh..",
      "..hhsssshh..",
      "..ssessess..",
      "..ssssssss..",
      "..sbbmmbbs..",
      "...bbbbbb...",
      ".....ss.....",
      "...cwwwwc...",
      "..cccccccc..",
      ".cacacacaca."
    ],
    "palette": {
      ".": "#FFF1DE",
      "h": "#D9B46A",
      "s": "#F2CDB4",
      "e": "#1B1B1B",
      "m": "#B5655A",
      "b": "#C9A05A",
      "c": "#8C2F39",
      "w": "#FFFFFF",
      "a": "#E8D7B8"
    }
  },
  "ayesha": {
    "name": "Ayesha",
    "map": [
      "....hhhh....",
      "...hhhhhh...",
      "..hhhhhhhh..",
      "..hhsssshh..",
      "..hsessesh..",
      "..hssssssh..",
      "..hssmmssh..",
      "..hhsssshh..",
      "..hhhhhhhh..",
      "..hhhhhhhh..",
      ".hhcccccchh.",
      "hhcccccccchh"
    ],
    "palette": {
      ".": "#F1ECFB",
      "h": "#7A2E4A",
      "s": "#B07A55",
      "e": "#1B1B1B",
      "m": "#7A3B2E",
      "c": "#2E3A59"
    }
  }
} as const;

export type Character = keyof typeof portraits;
