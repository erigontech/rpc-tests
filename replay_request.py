#!/usr/bin/python3
""" Replay JSON RPC API command """


import sys
from typing import List

from rpc.replay.config import OptionError
from rpc.replay.config import Options
from rpc.replay.player import Player


#
# main entry point
#
def main(argv: List[str]) -> int:
    """ Replay JSON RPC API command
    """
    try:
        player_options = Options(argv)
        player = Player(player_options)
        player.replay_request("engine_newPayloadV3", 1)
    except OptionError as err:
        print(err)
        Options.usage(argv)
        return 1

    return 0


#
# module as main
#
if __name__ == "__main__":
    sys.exit(main(sys.argv))
