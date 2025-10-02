#!/usr/bin/python3
""" Replay JSON RPC API command """


import sys
from typing import List

from rpc.replay import Options
from rpc.replay.player import Player


#
# main entry point
#
def main(argv: List[str]) -> int:
    """ Replay JSON RPC API command
    """
    player_options = Options()
    parse_error = player_options.parse(argv)
    if parse_error:
        print(parse_error, file=sys.stderr)
        Options.usage(argv)
        return 1

    player = Player(player_options)
    player.replay_request(player_options.method, player_options.method_index)

    return 0


#
# module as main
#
if __name__ == "__main__":
    sys.exit(main(sys.argv))
